package models

import (
	"crypto/sha256"
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
	fmt.Fprintf(h, "host_hash=%s\ncpu_model=%s\ncpu_flags=%s\ncpu_features=%d\ncpu_cores=%d\nl2=%d\nl3=%d\nfreq=%d\nos=%s\nkernel=%s\nkernel_string=%s\nsm=%s\ndeploy=%s",
		e.HostHash, e.CPUModel, e.CPUFlags, e.CPUFeatures, e.CPUCores, e.L2CacheKB, e.L3CacheKB, e.CPUFreqMHz, e.OS, e.KernelVersion, e.KernelString, e.SnakemakeVersion, e.DeployMode)
	return fmt.Sprintf("%x", h.Sum(nil))
}

// ShortHash returns the first 8 characters of the hash for display.
func (e Environment) ShortHash() string {
	if len(e.Hash) >= 8 {
		return e.Hash[:8]
	}
	return e.Hash
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
	InputSize      int64       `json:"input_size"`
	NumInputs      int         `json:"num_inputs"`
	InputType      string      `json:"input_type"`
	OutputSize     int64       `json:"output_size"`
	RuntimeSec     float64     `json:"runtime_sec"`
	Threads        int         `json:"threads"`
	MaxRSS         float64     `json:"max_rss_mb"`
	AvgCPUPercent  float64     `json:"cpu_percent"`
	ExitCode       int         `json:"exit_code"`
	LoadAvg        float64     `json:"load_avg"`     // 1-min load average at job start
	MemAvailMB     int         `json:"mem_avail_mb"` // available memory at job start
	SwapUsedMB     int         `json:"swap_used_mb"` // swap in use at job start
	IOWaitPct      float64     `json:"io_wait_pct"`  // iowait % during the job
	Timestamp      time.Time   `json:"timestamp"`
}

func (m ExecutionMetric) InputSizeHuman() string {
	b := float64(m.InputSize)
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", b/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", b/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", b/(1<<10))
	default:
		return fmt.Sprintf("%d B", m.InputSize)
	}
}

func (m ExecutionMetric) OutputSizeHuman() string {
	b := float64(m.OutputSize)
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", b/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", b/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", b/(1<<10))
	default:
		return fmt.Sprintf("%d B", m.OutputSize)
	}
}

// InputTypeClean returns the input type with a leading "." stripped.
func (m ExecutionMetric) InputTypeClean() string {
	return strings.TrimPrefix(m.InputType, ".")
}

// InputTypePrimary returns the longest (most specific) type for table views,
// with "[…]" appended if there are additional types.
func (m ExecutionMetric) InputTypePrimary() string {
	if m.InputType == "" {
		return ""
	}
	parts := strings.Split(m.InputType, ",")
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
	if m.InputType == "" {
		return ""
	}
	parts := strings.Split(m.InputType, ",")
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
