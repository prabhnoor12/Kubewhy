package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/kubewhy/kubewhy/internal/model"
)

// ChangeEvent describes a pod health status transition observed by the watch loop.
type ChangeEvent struct {
	From           string
	To             string
	DurationInPrev time.Duration
	Timestamp      time.Time
}

// stateTracker remembers the previous status and detects transitions.
// The first observation only establishes the baseline and emits no event.
type stateTracker struct {
	lastStatus string
	changedAt  time.Time
}

func newStateTracker() *stateTracker {
	return &stateTracker{}
}

// track returns a ChangeEvent when the status changed, or nil otherwise.
func (t *stateTracker) track(newStatus string, now time.Time) *ChangeEvent {
	if t.lastStatus == "" {
		t.lastStatus = newStatus
		t.changedAt = now
		return nil
	}
	if newStatus == t.lastStatus {
		return nil
	}
	event := &ChangeEvent{
		From:           t.lastStatus,
		To:             newStatus,
		DurationInPrev: now.Sub(t.changedAt),
		Timestamp:      now,
	}
	t.lastStatus = newStatus
	t.changedAt = now
	return event
}

func statusColor(status string) string {
	switch status {
	case "healthy":
		return "\033[32m"
	case "degraded":
		return "\033[33m"
	case "broken":
		return "\033[31m"
	default:
		return "\033[90m"
	}
}

func colorizeStatus(status string) string {
	return fmt.Sprintf("%s%s\033[0m", statusColor(status), strings.ToUpper(status))
}

func formatChange(event ChangeEvent, report model.Report) string {
	timestamp := event.Timestamp.Format("15:04:05")
	var b strings.Builder
	fmt.Fprintf(&b, "[%s] STATE CHANGE: %s → %s (was %s for %s)",
		timestamp, colorizeStatus(event.From), colorizeStatus(event.To),
		strings.ToUpper(event.From), formatDuration(event.DurationInPrev))
	if event.To != "healthy" && report.RootCause != nil {
		fmt.Fprintf(&b, "\n           Root cause: %s", report.RootCause.Title)
	}
	return b.String()
}

func formatDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		secs := int(d.Seconds())
		if secs <= 0 {
			return "less than a second"
		}
		return fmt.Sprintf("%ds", secs)
	case d < time.Hour:
		mins := int(d.Minutes())
		if mins == 1 {
			return "1m"
		}
		return fmt.Sprintf("%dm", mins)
	default:
		return d.Round(time.Minute).String()
	}
}
