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
	CPUFlags         string `json:"cpu_flags"`
	OS               string `json:"os"`
	KernelVersion    string `json:"kernel_version"`
	KernelString     string `json:"kernel_string"`
	SnakemakeVersion string `json:"sm_version"`
	DeployMode       string `json:"deploy_mode"`
}

// ComputeHash derives a deterministic SHA-256 hex digest from all key-value fields.
func (e *Environment) ComputeHash() string {
	h := sha256.New()
	fmt.Fprintf(h, "host_hash=%s\ncpu_model=%s\ncpu_flags=%s\nos=%s\nkernel=%s\nkernel_string=%s\nsm=%s\ndeploy=%s",
		e.HostHash, e.CPUModel, e.CPUFlags, e.OS, e.KernelVersion, e.KernelString, e.SnakemakeVersion, e.DeployMode)
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
	Tool           string      `json:"tool"`
	CommandPattern string      `json:"command"`
	Parameters     string      `json:"params"`
	InputSize      int64       `json:"input_size"`
	InputType      string      `json:"input_type"`
	RuntimeSec     float64     `json:"runtime_sec"`
	MaxRSS         float64     `json:"max_rss_mb"`
	AvgCPUPercent  float64     `json:"cpu_percent"`
	ExitCode       int         `json:"exit_code"`
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

// InputTypeClean returns the input type with a leading "." stripped.
func (m ExecutionMetric) InputTypeClean() string {
	return strings.TrimPrefix(m.InputType, ".")
}
