package models

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type Environment struct {
	ID               uint   `gorm:"primaryKey" json:"id"`
	Hash             string `gorm:"uniqueIndex;size:64" json:"hash"`
	HostHash         string `json:"host_hash"`
	CPUModel         string `json:"cpu_model"`
	CPUFlags         string `json:"cpu_flags"`    // deprecated: raw flag string from old clients
	CPUFeatures      uint64 `json:"cpu_features"` // bitmask of curated CPU features
	CPUCores         int    `json:"cpu_cores"`    // physical core count
	L2CacheKB        int    `json:"l2_cache_kb"`  // L2 cache size in KB
	L3CacheKB        int    `json:"l3_cache_kb"`  // L3 cache size in KB
	CPUFreqMHz       int    `json:"cpu_freq_mhz"` // max CPU frequency in MHz
	OS               string `json:"os"`
	KernelVersion    string `json:"kernel_version"`
	KernelString     string `json:"kernel_string"`
	SnakemakeVersion string `json:"sm_version"`
	DeployMode       string `json:"deploy_mode"`
}

// ComputeHash derives a deterministic SHA-256 hex digest from all key-value fields.
func (e *Environment) ComputeHash() string {
	h := sha256.New()
	fmt.Fprintf(h, "host_hash=%s\ncpu_model=%s\ncpu_features=%d\ncpu_cores=%d\nl2=%d\nl3=%d\nfreq=%d\nos=%s\nkernel=%s\nkernel_string=%s\nsm=%s\ndeploy=%s",
		e.HostHash, e.CPUModel, e.CPUFeatures, e.CPUCores, e.L2CacheKB, e.L3CacheKB, e.CPUFreqMHz, e.OS, e.KernelVersion, e.KernelString, e.SnakemakeVersion, e.DeployMode)
	return fmt.Sprintf("%x", h.Sum(nil))
}

// ShortHash returns the first 8 characters of the hash for display.
func (e Environment) ShortHash() string {
	if len(e.Hash) >= 8 {
		return e.Hash[:8]
	}
	return e.Hash
}

// Session holds workflow-level metadata, stored once per session_id.
type Session struct {
	ID              uint   `gorm:"primaryKey" json:"id"`
	SessionID       string `gorm:"uniqueIndex;not null" json:"session_id"`
	WorkflowURL     string `json:"workflow_url"`
	WorkflowVersion string `json:"workflow_version"`
}

// FileEntry represents a single input or output file with its type and size.
type FileEntry struct {
	Type string `json:"type"`
	Size int64  `json:"size"`
}

type ExecutionMetric struct {
	ID             uint        `gorm:"primaryKey" json:"id"`
	SessionID      string      `gorm:"uniqueIndex:idx_session_record;not null" json:"session_id"`
	RecordID       string      `gorm:"uniqueIndex:idx_session_record;not null" json:"record_id"`
	EnvironmentID  uint        `json:"env_id"`
	Environment    Environment `gorm:"foreignKey:EnvironmentID" json:"-"`
	Tool           string      `gorm:"index" json:"tool"`
	CommandPattern string      `json:"command"`
	Parameters     string      `json:"params"`
	ShellBlock     string      `json:"shell_block"`
	Inputs         string      `json:"inputs"`  // JSON-encoded []FileEntry
	Outputs        string      `json:"outputs"` // JSON-encoded []FileEntry
	RuntimeSec     float64     `json:"runtime_sec"`
	Threads        int         `json:"threads"`
	MaxRSS         float64     `json:"max_rss_mb"`
	AvgCPUPercent  float64     `json:"cpu_percent"`
	MaxVMS         float64     `json:"max_vms_mb"`
	MaxUSS         float64     `json:"max_uss_mb"`
	MaxPSS         float64     `json:"max_pss_mb"`
	IOIn           float64     `json:"io_in_mb"`
	IOOut          float64     `json:"io_out_mb"`
	CPUTime        float64     `json:"cpu_time_sec"`
	Resources      string      `json:"resources"` // JSON-encoded dict
	ToolVersion    string      `json:"tool_version"`
	Category       string      `json:"category"`
	ExitCode       int         `json:"exit_code"`
	LoadAvg        float64     `json:"load_avg"`
	MemAvailMB     int         `json:"mem_avail_mb"`
	SwapUsedMB     int         `json:"swap_used_mb"`
	IOWaitPct      float64     `json:"io_wait_pct"`
	Timestamp      time.Time   `json:"timestamp"`
}

// ParsedInputs deserializes the Inputs JSON column into a slice of FileEntry.
func (m ExecutionMetric) ParsedInputs() []FileEntry {
	var entries []FileEntry
	if m.Inputs != "" {
		json.Unmarshal([]byte(m.Inputs), &entries)
	}
	return entries
}

// ParsedOutputs deserializes the Outputs JSON column into a slice of FileEntry.
func (m ExecutionMetric) ParsedOutputs() []FileEntry {
	var entries []FileEntry
	if m.Outputs != "" {
		json.Unmarshal([]byte(m.Outputs), &entries)
	}
	return entries
}

// TotalInputSize returns the sum of all input file sizes in bytes.
func (m ExecutionMetric) TotalInputSize() int64 {
	var total int64
	for _, e := range m.ParsedInputs() {
		total += e.Size
	}
	return total
}

// TotalOutputSize returns the sum of all output file sizes in bytes.
func (m ExecutionMetric) TotalOutputSize() int64 {
	var total int64
	for _, e := range m.ParsedOutputs() {
		total += e.Size
	}
	return total
}

// NumInputs returns the number of input files.
func (m ExecutionMetric) NumInputs() int {
	return len(m.ParsedInputs())
}

// NumOutputs returns the number of output files.
func (m ExecutionMetric) NumOutputs() int {
	return len(m.ParsedOutputs())
}

// InputTypes returns comma-separated unique input file types.
func (m ExecutionMetric) InputTypes() string {
	seen := map[string]bool{}
	var types []string
	for _, e := range m.ParsedInputs() {
		if e.Type != "" && !seen[e.Type] {
			seen[e.Type] = true
			types = append(types, e.Type)
		}
	}
	return strings.Join(types, ",")
}

func humanSize(b int64) string {
	fb := float64(b)
	switch {
	case fb >= 1<<30:
		return fmt.Sprintf("%.1f GB", fb/(1<<30))
	case fb >= 1<<20:
		return fmt.Sprintf("%.1f MB", fb/(1<<20))
	case fb >= 1<<10:
		return fmt.Sprintf("%.1f KB", fb/(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func (m ExecutionMetric) InputSizeHuman() string {
	return humanSize(m.TotalInputSize())
}

func (m ExecutionMetric) OutputSizeHuman() string {
	return humanSize(m.TotalOutputSize())
}

// InputTypeClean returns the input types with leading "." stripped.
func (m ExecutionMetric) InputTypeClean() string {
	return strings.TrimPrefix(m.InputTypes(), ".")
}

// InputTypePrimary returns the longest (most specific) type for table views,
// with "[…]" appended if there are additional types.
func (m ExecutionMetric) InputTypePrimary() string {
	types := m.InputTypes()
	if types == "" {
		return ""
	}
	parts := strings.Split(types, ",")
	best := ""
	for _, p := range parts {
		p = strings.TrimPrefix(strings.TrimSpace(p), ".")
		if len(p) > len(best) {
			best = p
		}
	}
	if len(parts) > 1 {
		return best + " […]"
	}
	return best
}

// InputTypeList returns all types formatted as "[typea typeb typec]".
func (m ExecutionMetric) InputTypeList() string {
	types := m.InputTypes()
	if types == "" {
		return ""
	}
	parts := strings.Split(types, ",")
	var cleaned []string
	for _, p := range parts {
		p = strings.TrimPrefix(strings.TrimSpace(p), ".")
		if p != "" {
			cleaned = append(cleaned, p)
		}
	}
	if len(cleaned) == 0 {
		return ""
	}
	if len(cleaned) == 1 {
		return cleaned[0]
	}
	return "[" + strings.Join(cleaned, " ") + "]"
}
