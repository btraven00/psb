package handlers

import (
	"bufio"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/btraven00/psb/internal/cpufeatures"
	"github.com/btraven00/psb/internal/models"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

// RecordPayload is a single JSONL line within a batch submission.
type RecordPayload struct {
	// Session & record identification
	SessionID string `json:"session_id"`
	RecordID  string `json:"record_id"`

	// Environment fields (repeated per line, server deduplicates by hash)
	HostHash         string `json:"host_hash"`
	CPUModel         string `json:"cpu_model"`
	CPUFlags         string `json:"cpu_flags"`    // deprecated: raw flag string
	CPUFeatures      uint64 `json:"cpu_features"` // bitmask of curated features
	CPUCores         int    `json:"cpu_cores"`
	L2CacheKB        int    `json:"l2_cache_kb"`
	L3CacheKB        int    `json:"l3_cache_kb"`
	CPUFreqMHz       int    `json:"cpu_freq_mhz"`
	OS               string `json:"os"`
	KernelVersion    string `json:"kernel_version"`
	KernelString     string `json:"kernel_string"`
	SnakemakeVersion string `json:"sm_version"`
	DeployMode       string `json:"deploy_mode"`

	// Metric fields
	Tool           string  `json:"tool"`
	CommandPattern string  `json:"command"`
	Parameters     string  `json:"params"`
	InputSize      int64   `json:"input_size"`
	NumInputs      int     `json:"num_inputs"`
	InputType      string  `json:"input_type"`
	OutputSize     int64   `json:"output_size"`
	RuntimeSec     float64 `json:"runtime_sec"`
	Threads        int     `json:"threads"`
	MaxRSS         float64 `json:"max_rss_mb"`
	AvgCPUPercent  float64 `json:"cpu_percent"`
	ExitCode       int     `json:"exit_code"`
	LoadAvg        float64 `json:"load_avg"`
	MemAvailMB     int     `json:"mem_avail_mb"`
	SwapUsedMB     int     `json:"swap_used_mb"`
	IOWaitPct      float64 `json:"io_wait_pct"`
}

type Handler struct {
	DB       *gorm.DB
	PSBToken string // shared secret for X-PSB-Token header validation
}

// teapot returns a 418 I'm a teapot response.
func teapot(c echo.Context) error {
	return c.JSON(http.StatusTeapot, map[string]string{"error": "I'm a teapot"})
}

// absolutePathPatterns are rejected in the params field.
var absolutePathPatterns = []string{"/home/", "/Users/", "C:\\", "/tmp/"}

type lineError struct {
	Line  int    `json:"line"`
	Error string `json:"error"`
}

// validateRecord checks a single record and returns an error string, or "" if valid.
func validateRecord(p *RecordPayload) string {
	if strings.TrimSpace(p.SessionID) == "" {
		return "session_id is required"
	}
	if strings.TrimSpace(p.RecordID) == "" {
		return "record_id is required"
	}
	if strings.TrimSpace(p.Tool) == "" {
		return "tool is required"
	}
	if p.RuntimeSec <= 0 {
		return "runtime_sec must be > 0"
	}
	for _, pat := range absolutePathPatterns {
		if strings.Contains(p.Parameters, pat) {
			return "params must not contain absolute paths"
		}
	}
	return ""
}

const maxBodySize = 10 << 20 // 10 MiB

func (h *Handler) PostTelemetry(c echo.Context) error {
	// --- Gate checks (418 on failure) ---

	// Gate 1: X-PSB-Token header must match the server secret
	if c.Request().Header.Get("X-PSB-Token") != h.PSBToken {
		return teapot(c)
	}

	// Gate 2: _psb_session cookie must be present and non-empty
	cookie, err := c.Cookie("_psb_session")
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return teapot(c)
	}

	// Gate 3: X-PSB-Nonce header must be non-empty
	if strings.TrimSpace(c.Request().Header.Get("X-PSB-Nonce")) == "" {
		return teapot(c)
	}

	// --- Parse JSONL body, one record per line ---
	body := http.MaxBytesReader(c.Response(), c.Request().Body, maxBodySize)
	defer body.Close()

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20) // up to 1 MiB per line

	// First pass: parse all lines
	type parsedLine struct {
		lineNum int
		payload RecordPayload
	}
	var (
		parsed   []parsedLine
		rejected int
		errors   []lineError
		lineNum  int
	)

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var p RecordPayload
		if err := json.Unmarshal([]byte(line), &p); err != nil {
			rejected++
			errors = append(errors, lineError{Line: lineNum, Error: "invalid JSON"})
			continue
		}

		if errMsg := validateRecord(&p); errMsg != "" {
			rejected++
			errors = append(errors, lineError{Line: lineNum, Error: errMsg})
			continue
		}

		parsed = append(parsed, parsedLine{lineNum: lineNum, payload: p})
	}

	if err := scanner.Err(); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "failed to read request body"})
	}

	if lineNum == 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "empty payload"})
	}

	// Second pass: insert within a single transaction
	var (
		accepted   int
		duplicates int
		envCache   = make(map[string]uint) // env hash -> env ID
	)

	txErr := h.DB.Transaction(func(tx *gorm.DB) error {
		for _, pl := range parsed {
			p := pl.payload

			// Check for duplicate (session_id + record_id)
			var existing models.ExecutionMetric
			if err := tx.Where("session_id = ? AND record_id = ?", p.SessionID, p.RecordID).First(&existing).Error; err == nil {
				duplicates++
				continue
			}

			// Upsert environment by hash (cached per request)
			features := p.CPUFeatures
			if features == 0 && p.CPUFlags != "" {
				// Backfill: old client sent raw flags, encode them
				features = cpufeatures.Encode(p.CPUFlags)
			}
			env := models.Environment{
				HostHash:         p.HostHash,
				CPUModel:         p.CPUModel,
				CPUFlags:         p.CPUFlags,
				CPUFeatures:      features,
				CPUCores:         p.CPUCores,
				L2CacheKB:        p.L2CacheKB,
				L3CacheKB:        p.L3CacheKB,
				CPUFreqMHz:       p.CPUFreqMHz,
				OS:               p.OS,
				KernelVersion:    p.KernelVersion,
				KernelString:     p.KernelString,
				SnakemakeVersion: p.SnakemakeVersion,
				DeployMode:       p.DeployMode,
			}
			envHash := env.ComputeHash()

			envID, cached := envCache[envHash]
			if !cached {
				env.Hash = envHash
				if err := tx.Where("hash = ?", envHash).FirstOrCreate(&env).Error; err != nil {
					rejected++
					errors = append(errors, lineError{Line: pl.lineNum, Error: "db error"})
					continue
				}
				envID = env.ID
				envCache[envHash] = envID
			}

			metric := models.ExecutionMetric{
				SessionID:      p.SessionID,
				RecordID:       p.RecordID,
				EnvironmentID:  envID,
				Tool:           p.Tool,
				CommandPattern: p.CommandPattern,
				Parameters:     p.Parameters,
				InputSize:      p.InputSize,
				NumInputs:      p.NumInputs,
				InputType:      p.InputType,
				OutputSize:     p.OutputSize,
				RuntimeSec:     p.RuntimeSec,
				Threads:        p.Threads,
				MaxRSS:         p.MaxRSS,
				AvgCPUPercent:  p.AvgCPUPercent,
				ExitCode:       p.ExitCode,
				LoadAvg:        p.LoadAvg,
				MemAvailMB:     p.MemAvailMB,
				SwapUsedMB:     p.SwapUsedMB,
				IOWaitPct:      p.IOWaitPct,
				Timestamp:      time.Now().UTC(),
			}
			if err := tx.Create(&metric).Error; err != nil {
				rejected++
				errors = append(errors, lineError{Line: pl.lineNum, Error: "db error"})
				continue
			}

			accepted++
		}
		return nil
	})

	if txErr != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "transaction failed"})
	}

	return c.JSON(http.StatusCreated, map[string]interface{}{
		"status":     "ok",
		"accepted":   accepted,
		"duplicates": duplicates,
		"rejected":   rejected,
		"errors":     errors,
	})
}
