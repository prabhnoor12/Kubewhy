package cli

import (
	"testing"

	"github.com/kubewhy/kubewhy/internal/model"
)

func TestReportExitCodeForStatus(t *testing.T) {
	tests := map[string]int{
		"healthy":  0,
		"degraded": 1,
		"broken":   2,
		"unknown":  3,
		"other":    3,
	}
	for status, want := range tests {
		if got := reportExitCodeForStatus(status); got != want {
			t.Errorf("reportExitCodeForStatus(%q) = %d, want %d", status, got, want)
		}
	}
}

func TestReportExitCodeUsesReportStatus(t *testing.T) {
	if got := reportExitCode(model.Report{Status: "broken"}); got != 2 {
		t.Fatalf("reportExitCode(broken) = %d, want 2", got)
	}
}
