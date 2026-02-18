package task

import (
	"time"

	"github.com/twiced-technology-gmbh/agentwatch/internal/config"
)

// UpdateTimestamps sets Started and Completed based on the status transition.
//   - Sets Started on first move out of initial status (never overwrites).
//   - Sets Completed on move to terminal status; also sets Started if nil.
//   - Clears Completed when moving away from terminal status (reopening).
func UpdateTimestamps(t *Task, oldStatus, newStatus string, cfg *config.Config) {
	now := time.Now()
	initialStatus := cfg.StatusNames()[0]

	// Set Started on first move out of initial status (never overwrite).
	if t.Started == nil && oldStatus == initialStatus && newStatus != initialStatus {
		t.Started = &now
	}

	// Set/clear Completed based on terminal status.
	if cfg.IsTerminalStatus(newStatus) {
		t.Completed = &now
		// Direct move to terminal: also set Started if nil.
		if t.Started == nil {
			t.Started = &now
		}
	} else if cfg.IsTerminalStatus(oldStatus) {
		// Reopening: clear Completed, preserve Started.
		t.Completed = nil
	}
}
