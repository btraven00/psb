package handlers

import (
	"bufio"
	"encoding/json"
	"net/http"
	"strings"
	"time"

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
	CPUFlags         string `json:"cpu_flags"`
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
	InputType      string  `json:"input_type"`
	RuntimeSec     float64 `json:"runtime_sec"`
	MaxRSS         float64 `json:"max_rss_mb"`
	AvgCPUPercent  float64 `json:"cpu_percent"`
	ExitCode       int     `json:"exit_code"`
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

	var (
		accepted   int
		duplicates int
		rejected   int
		errors     []lineError
		lineNum    int

		// Cache environment upserts within this request
		envCache = make(map[string]uint) // env hash -> env ID
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

		// Check for duplicate (session_id + record_id)
		var existing models.ExecutionMetric
		if err := h.DB.Where("session_id = ? AND record_id = ?", p.SessionID, p.RecordID).First(&existing).Error; err == nil {
			duplicates++
			continue
		}

		// Upsert environment by hash (cached per request)
		env := models.Environment{
			HostHash:         p.HostHash,
			CPUModel:         p.CPUModel,
			CPUFlags:         p.CPUFlags,
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
			if err := h.DB.Where("hash = ?", envHash).FirstOrCreate(&env).Error; err != nil {
				rejected++
				errors = append(errors, lineError{Line: lineNum, Error: "db error"})
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
			InputType:      p.InputType,
			RuntimeSec:     p.RuntimeSec,
			MaxRSS:         p.MaxRSS,
			AvgCPUPercent:  p.AvgCPUPercent,
			ExitCode:       p.ExitCode,
			Timestamp:      time.Now().UTC(),
		}
		if err := h.DB.Create(&metric).Error; err != nil {
			rejected++
			errors = append(errors, lineError{Line: lineNum, Error: "db error"})
			continue
		}

		accepted++
	}

	if err := scanner.Err(); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "failed to read request body"})
	}

	if lineNum == 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "empty payload"})
	}

	return c.JSON(http.StatusCreated, map[string]interface{}{
		"status":     "ok",
		"accepted":   accepted,
		"duplicates": duplicates,
		"rejected":   rejected,
		"errors":     errors,
	})
}
