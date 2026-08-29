package diagnosis

import (
	"strings"
	"testing"

	"github.com/kubewhy/kubewhy/internal/model"
)

func TestDiagnoseCrashLoopCorrelatesLogsAndEvents(t *testing.T) {
	report := NewEngine().Diagnose(model.DiagnoseRequest{
		Pod:    model.Pod{Metadata: model.ObjectMeta{Name: "api", Namespace: "payments"}, Status: model.PodStatus{Phase: "Running", ContainerStatuses: []model.ContainerStatus{{Name: "api", RestartCount: 7, State: model.ContainerState{Waiting: &model.WaitingState{Reason: "CrashLoopBackOff", Message: "back-off 5m"}}, LastState: model.ContainerState{Terminated: &model.TerminatedState{ExitCode: 1, Reason: "Error"}}}}}},
		Events: []model.Event{{Type: "Warning", Reason: "BackOff", Message: "Back-off restarting failed container", Count: 8}},
		Logs:   []model.ContainerLog{{Container: "api", Previous: true, Text: "panic: database unavailable"}},
	})
	if report.Status != "broken" {
		t.Fatalf("status = %q, want broken", report.Status)
	}
	for _, code := range []string{"crash_loop", "event_backoff", "log_panic", "log_dependency_unavailable"} {
		if !hasReason(report, code) {
			t.Errorf("missing reason %q", code)
		}
	}
	if report.Containers[0].RestartCount != 7 {
		t.Errorf("restart count not preserved")
	}
}

func TestDiagnoseResourceContext(t *testing.T) {
	report := NewEngine().Diagnose(model.DiagnoseRequest{
		Pod:       model.Pod{Metadata: model.ObjectMeta{Name: "worker"}, Spec: model.PodSpec{Containers: []model.Container{{Name: "worker", Resources: model.ResourceRequirements{Requests: map[string]string{"cpu": "500m", "memory": "512Mi"}}}}}},
		Resources: model.ResourceContext{Nodes: []model.NodeCapacity{{Name: "small", Schedulable: true, AvailableCPUMillicores: 250, AvailableMemoryBytes: 128 << 20}}},
	})
	if !hasReason(report, "resource_insufficient_cpu") || !hasReason(report, "resource_insufficient_memory") {
		t.Fatalf("resource reasons missing: %#v", report.Reasons)
	}
}

func TestHealthyPod(t *testing.T) {
	report := NewEngine().Diagnose(model.DiagnoseRequest{Pod: model.Pod{Metadata: model.ObjectMeta{Name: "api"}, Status: model.PodStatus{Phase: "Running", ContainerStatuses: []model.ContainerStatus{{Name: "api", Ready: true, State: model.ContainerState{Running: &model.RunningState{}}}}}}})
	if report.Status != "healthy" || !strings.Contains(report.Summary, "healthy") {
		t.Fatalf("unexpected report: %#v", report)
	}
}

func hasReason(report model.Report, code string) bool {
	for _, reason := range report.Reasons {
		if reason.Code == code {
			return true
		}
	}
	return false
}
