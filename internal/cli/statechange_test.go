package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/kubewhy/kubewhy/internal/model"
)

func TestStateTrackerDetectsChange(t *testing.T) {
	tracker := newStateTracker()
	now := time.Now()

	if event := tracker.track("healthy", now); event != nil {
		t.Errorf("first track() should return nil, got %+v", event)
	}
	if event := tracker.track("broken", now.Add(2*time.Minute)); event == nil {
		t.Fatal("second track() should return a ChangeEvent")
	} else {
		if event.From != "healthy" || event.To != "broken" {
			t.Errorf("From=%q To=%q, want healthy→broken", event.From, event.To)
		}
		if event.DurationInPrev != 2*time.Minute {
			t.Errorf("DurationInPrev=%v, want 2m", event.DurationInPrev)
		}
	}
}

func TestStateTrackerNoChange(t *testing.T) {
	tracker := newStateTracker()
	now := time.Now()

	tracker.track("healthy", now)
	if event := tracker.track("healthy", now.Add(5*time.Minute)); event != nil {
		t.Errorf("same status should return nil, got %+v", event)
	}
}

func TestStateTrackerThreeTransitions(t *testing.T) {
	tracker := newStateTracker()
	now := time.Now()

	tracker.track("healthy", now)

	event := tracker.track("broken", now.Add(3*time.Minute))
	if event == nil || event.From != "healthy" || event.To != "broken" {
		t.Fatalf("first change = %+v, want healthy→broken", event)
	}

	event = tracker.track("degraded", now.Add(5*time.Minute))
	if event == nil || event.From != "broken" || event.To != "degraded" {
		t.Fatalf("second change = %+v, want broken→degraded", event)
	}
	if event.DurationInPrev != 2*time.Minute {
		t.Errorf("DurationInPrev=%v, want 2m", event.DurationInPrev)
	}

	event = tracker.track("healthy", now.Add(8*time.Minute))
	if event == nil || event.From != "degraded" || event.To != "healthy" {
		t.Fatalf("third change = %+v, want degraded→healthy", event)
	}
}

func TestStateTrackerEmptyThenSameStatus(t *testing.T) {
	tracker := newStateTracker()
	now := time.Now()

	if event := tracker.track("", now); event != nil {
		t.Errorf("empty status on first track should return nil, got %+v", event)
	}
}

func TestFormatChangeIncludesRootCause(t *testing.T) {
	event := ChangeEvent{
		From:           "healthy",
		To:             "broken",
		DurationInPrev: 2 * time.Minute,
		Timestamp:      time.Date(2026, 9, 25, 14, 32, 1, 0, time.UTC),
	}
	report := model.Report{
		Status: "broken",
		RootCause: &model.Reason{
			Code:  "crash_loop",
			Title: "Container is crash-looping",
		},
	}
	output := formatChange(event, report)
	if !strings.Contains(output, "Container is crash-looping") {
		t.Errorf("output should include root cause title, got: %q", output)
	}
	if !strings.Contains(output, "14:32:01") {
		t.Errorf("output should include timestamp, got: %q", output)
	}
	if !strings.Contains(output, "HEALTHY") || !strings.Contains(output, "BROKEN") {
		t.Errorf("output should include both statuses, got: %q", output)
	}
	if !strings.Contains(output, "was HEALTHY for 2m") {
		t.Errorf("output should include duration in previous state, got: %q", output)
	}
}

func TestFormatChangeHealthyRecoveryOmitsRootCause(t *testing.T) {
	event := ChangeEvent{
		From:           "broken",
		To:             "healthy",
		DurationInPrev: 3 * time.Minute,
		Timestamp:      time.Now(),
	}
	report := model.Report{
		Status: "healthy",
		RootCause: &model.Reason{
			Code:  "crash_loop",
			Title: "Container is crash-looping",
		},
	}
	output := formatChange(event, report)
	if strings.Contains(output, "Root cause") {
		t.Errorf("recovery output should not include root cause, got: %q", output)
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		input time.Duration
		want  string
	}{
		{0, "less than a second"},
		{500 * time.Millisecond, "less than a second"},
		{30 * time.Second, "30s"},
		{90 * time.Second, "1m"},
		{5 * time.Minute, "5m"},
		{90 * time.Minute, "1h30m0s"},
		{2 * time.Hour, "2h0m0s"},
	}
	for _, test := range tests {
		if got := formatDuration(test.input); got != test.want {
			t.Errorf("formatDuration(%v) = %q, want %q", test.input, got, test.want)
		}
	}
}

func TestColorizeStatus(t *testing.T) {
	tests := map[string]string{
		"healthy":  "\033[32m",
		"degraded": "\033[33m",
		"broken":   "\033[31m",
		"unknown":  "\033[90m",
	}
	for status, color := range tests {
		output := colorizeStatus(status)
		if !strings.HasPrefix(output, color) {
			t.Errorf("colorizeStatus(%q) = %q, should start with %q", status, output, color)
		}
		if !strings.HasSuffix(output, "\033[0m") {
			t.Errorf("colorizeStatus(%q) = %q, should end with reset code", status, output)
		}
		if !strings.Contains(output, strings.ToUpper(status)) {
			t.Errorf("colorizeStatus(%q) = %q, should contain %q", status, output, strings.ToUpper(status))
		}
	}
}
