package output

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/twiced-technology-gmbh/agentwatch/internal/board"
	"github.com/twiced-technology-gmbh/agentwatch/internal/task"
)

// TaskCompact renders a list of tasks in one-line-per-record compact format.
func TaskCompact(w io.Writer, tasks []*task.Task) {
	if len(tasks) == 0 {
		fmt.Fprintln(os.Stderr, "No tasks found.")
		return
	}

	for _, t := range tasks {
		fmt.Fprintln(w, formatTaskLine(t))
	}
}

// TaskDetailCompact renders a single task with detail in compact format.
func TaskDetailCompact(w io.Writer, t *task.Task) {
	line := formatTaskLine(t)
	if t.Estimate != "" {
		line += " est:" + t.Estimate
	}
	fmt.Fprintln(w, line)

	// Timestamps line.
	ts := "  created:" + t.Created.Format("2006-01-02") +
		" updated:" + t.Updated.Format("2006-01-02")
	if t.Started != nil {
		ts += " started:" + t.Started.Format("2006-01-02")
	}
	if t.Completed != nil {
		ts += " completed:" + t.Completed.Format("2006-01-02")
	}
	fmt.Fprintln(w, ts)

	if t.Body != "" {
		for _, bodyLine := range strings.Split(t.Body, "\n") {
			fmt.Fprintln(w, "  "+bodyLine)
		}
	}
}

// OverviewCompact renders a board summary in compact format.
func OverviewCompact(w io.Writer, s board.Overview) {
	fmt.Fprintf(w, "%s (%d tasks)\n", s.BoardName, s.TotalTasks)

	for _, ss := range s.Statuses {
		line := "  " + ss.Status + ": " + strconv.Itoa(ss.Count)
		if ss.WIPLimit > 0 {
			line += "/" + strconv.Itoa(ss.WIPLimit)
		}
		var annotations []string
		if ss.Blocked > 0 {
			annotations = append(annotations, strconv.Itoa(ss.Blocked)+" blocked")
		}
		if ss.Overdue > 0 {
			annotations = append(annotations, strconv.Itoa(ss.Overdue)+" overdue")
		}
		if len(annotations) > 0 {
			line += " (" + strings.Join(annotations, ", ") + ")"
		}
		fmt.Fprintln(w, line)
	}

	if len(s.Priorities) > 0 {
		parts := make([]string, 0, len(s.Priorities))
		for _, pc := range s.Priorities {
			parts = append(parts, pc.Priority+"="+strconv.Itoa(pc.Count))
		}
		fmt.Fprintln(w, "Priority: "+strings.Join(parts, " "))
	}
}


// formatTaskLine builds the one-line representation of a task.
func formatTaskLine(t *task.Task) string {
	line := "#" + strconv.Itoa(t.ID) + " [" + t.Status + "/" + t.Priority + "] " + t.Title

	if t.ClaimedBy != "" {
		line += " @" + t.ClaimedBy
	}
	if len(t.Tags) > 0 {
		line += " (" + strings.Join(t.Tags, ", ") + ")"
	}
	if t.Due != nil {
		line += " due:" + t.Due.String()
	}

	return line
}

