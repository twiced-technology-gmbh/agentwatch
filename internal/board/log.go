package board

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)


const (
	logFileName   = "activity.jsonl"
	logFileMode   = 0o600
	maxLogEntries = 10000 // truncate oldest entries when log exceeds this size
)

// LogEntry represents a single activity log entry.
type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Action    string    `json:"action"`
	TaskID    int       `json:"task_id"`
	Detail    string    `json:"detail"`
}

// AppendLog appends a log entry to the activity log file.
// If the log exceeds maxLogEntries, the oldest entries are truncated.
func AppendLog(kanbanDir string, entry LogEntry) error {
	path := filepath.Join(kanbanDir, logFileName)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, logFileMode) //nolint:gosec // log path from trusted kanban dir
	if err != nil {
		return fmt.Errorf("opening log file: %w", err)
	}
	defer f.Close()

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshaling log entry: %w", err)
	}

	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("writing log entry: %w", err)
	}

	// Truncate if needed (best-effort; errors are non-fatal).
	_ = truncateLogIfNeeded(path)

	return nil
}

// truncateLogIfNeeded reads the log file and, if it exceeds maxLogEntries,
// rewrites it keeping only the most recent entries.
func truncateLogIfNeeded(path string) error {
	f, err := os.Open(path) //nolint:gosec // trusted path
	if err != nil {
		return err
	}

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	_ = f.Close()

	if err := scanner.Err(); err != nil {
		return err
	}

	if len(lines) <= maxLogEntries {
		return nil
	}

	// Keep only the last maxLogEntries lines.
	lines = lines[len(lines)-maxLogEntries:]

	var buf strings.Builder
	for _, line := range lines {
		buf.WriteString(line)
		buf.WriteByte('\n')
	}

	return os.WriteFile(path, []byte(buf.String()), logFileMode)
}

// LogMutation appends an activity log entry. Errors are silently discarded
// because logging should never fail a command.
func LogMutation(kanbanDir, action string, taskID int, detail string) {
	entry := LogEntry{
		Timestamp: time.Now(),
		Action:    action,
		TaskID:    taskID,
		Detail:    detail,
	}
	_ = AppendLog(kanbanDir, entry)
}

