// Package artifact collects and stores run outputs: logs, command results,
// terraform plans, and any additional files.
package artifact

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/stormbane-security/drydock/internal/runner"
)

// RunRecord is the complete output of a drydock run.
type RunRecord struct {
	ID            string            `json:"id"`
	Scenario      string            `json:"scenario"`
	StartedAt     time.Time         `json:"started_at"`
	FinishedAt    time.Time         `json:"finished_at"`
	Duration      time.Duration     `json:"duration"`
	Status        string            `json:"status"` // "pass", "fail", "error"
	CommandResults []runner.Result   `json:"command_results"`
	AssertionResults []AssertionResult `json:"assertion_results,omitempty"`
	Logs          map[string]string `json:"logs,omitempty"`
	Outputs       map[string]string `json:"outputs,omitempty"`
	Error         string            `json:"error,omitempty"`
}

// AssertionResult records whether an assertion passed or failed.
type AssertionResult struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Passed  bool   `json:"passed"`
	Message string `json:"message,omitempty"`
}

// Store manages artifact storage on disk.
type Store struct {
	baseDir string // e.g. ".drydock/runs"
}

// NewStore creates a store rooted at the given directory.
func NewStore(baseDir string) *Store {
	return &Store{baseDir: baseDir}
}

// Save writes a RunRecord to disk as JSON.
func (s *Store) Save(record *RunRecord) error {
	dir := filepath.Join(s.baseDir, record.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating artifact dir: %w", err)
	}

	// Write the main record.
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling record: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run.json"), data, 0o644); err != nil {
		return fmt.Errorf("writing run.json: %w", err)
	}

	// Write logs to individual files.
	if len(record.Logs) > 0 {
		logsDir := filepath.Join(dir, "logs")
		if err := os.MkdirAll(logsDir, 0o755); err != nil {
			return fmt.Errorf("creating logs dir: %w", err)
		}
		for name, content := range record.Logs {
			logFile := filepath.Join(logsDir, sanitizeFilename(name)+".log")
			os.WriteFile(logFile, []byte(content), 0o644) //nolint:errcheck
		}
	}

	return nil
}

// Load reads a RunRecord from disk.
func (s *Store) Load(runID string) (*RunRecord, error) {
	path := filepath.Join(s.baseDir, runID, "run.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading run record: %w", err)
	}

	var record RunRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, fmt.Errorf("parsing run record: %w", err)
	}
	return &record, nil
}

// List returns all run IDs.
func (s *Store) List() ([]string, error) {
	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			ids = append(ids, e.Name())
		}
	}
	return ids, nil
}

// SaveFile writes an additional file into a run's artifact directory.
func (s *Store) SaveFile(runID, name string, data []byte) error {
	dir := filepath.Join(s.baseDir, runID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, sanitizeFilename(name)), data, 0o644)
}

func sanitizeFilename(name string) string {
	// Replace path separators and other problematic characters.
	r := []byte(name)
	for i, b := range r {
		switch b {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			r[i] = '_'
		}
	}
	return string(r)
}

// GenerateRunID creates a run ID from scenario name and timestamp.
func GenerateRunID(scenarioName string) string {
	ts := time.Now().UTC().Format("20060102-150405")
	return fmt.Sprintf("%s-%s", sanitizeFilename(scenarioName), ts)
}
