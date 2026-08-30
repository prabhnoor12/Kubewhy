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
	if report.RootCause == nil || report.RootCause.Code != "log_dependency_unavailable" {
		t.Fatalf("root cause = %#v, want dependency failure", report.RootCause)
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
	if !hasReason(report, "resource_no_feasible_node") {
		t.Fatalf("resource reasons missing: %#v", report.Reasons)
	}
}

func TestDiagnoseResourceContextFindsAnyFeasibleNode(t *testing.T) {
	report := NewEngine().Diagnose(model.DiagnoseRequest{
		Pod: model.Pod{Metadata: model.ObjectMeta{Name: "worker"}, Spec: model.PodSpec{Containers: []model.Container{{Name: "worker", Resources: model.ResourceRequirements{Requests: map[string]string{"cpu": "500m", "memory": "512Mi"}}}}}},
		Resources: model.ResourceContext{Nodes: []model.NodeCapacity{
			{Name: "too-small", Schedulable: true, AvailableCPUMillicores: 250, AvailableMemoryBytes: 1 << 30},
			{Name: "large-enough", Schedulable: true, AvailableCPUMillicores: 1000, AvailableMemoryBytes: 1 << 30},
		}},
	})
	if hasReason(report, "resource_no_feasible_node") {
		t.Fatalf("reported no feasible node despite a matching node: %#v", report.ResourceFindings)
	}
}

func TestDiagnoseAggregatesEvidenceForDuplicateReasons(t *testing.T) {
	report := NewEngine().Diagnose(model.DiagnoseRequest{
		Pod:    model.Pod{Metadata: model.ObjectMeta{Name: "api"}, Spec: model.PodSpec{Containers: []model.Container{{Name: "api"}, {Name: "worker"}}}, Status: model.PodStatus{Phase: "Running"}},
		Events: []model.Event{},
		Logs: []model.ContainerLog{
			{Container: "api", Text: "connection refused"},
			{Container: "worker", Text: "connection refused"},
		},
	})
	reason := findReason(report, "log_dependency_unavailable")
	if reason == nil {
		t.Fatal("missing dependency log reason")
	}
	for _, container := range []string{"container=api", "container=worker"} {
		if !contains(reason.Evidence, container) {
			t.Errorf("evidence does not contain %q: %#v", container, reason.Evidence)
		}
	}
}

func TestHealthyPod(t *testing.T) {
	report := NewEngine().Diagnose(model.DiagnoseRequest{
		Pod: model.Pod{
			Metadata: model.ObjectMeta{Name: "api"},
			Spec:     model.PodSpec{Containers: []model.Container{{Name: "api", Resources: model.ResourceRequirements{Requests: map[string]string{"cpu": "10m", "memory": "16Mi"}}}}},
			Status:   model.PodStatus{Phase: "Running", ContainerStatuses: []model.ContainerStatus{{Name: "api", Ready: true, State: model.ContainerState{Running: &model.RunningState{}}}}},
		},
		Events: []model.Event{},
		Logs:   []model.ContainerLog{},
	})
	if report.Status != "healthy" || report.Confidence != "high" || !strings.Contains(report.Summary, "healthy") {
		t.Fatalf("unexpected report: %#v", report)
	}
}

func TestIncompleteContextIsUnknown(t *testing.T) {
	report := NewEngine().Diagnose(model.DiagnoseRequest{
		Pod: model.Pod{
			Metadata: model.ObjectMeta{Name: "api"},
			Spec:     model.PodSpec{Containers: []model.Container{{Name: "api", Resources: model.ResourceRequirements{Requests: map[string]string{"cpu": "10m", "memory": "16Mi"}}}}},
			Status:   model.PodStatus{Phase: "Running", ContainerStatuses: []model.ContainerStatus{{Name: "api", Ready: true, State: model.ContainerState{Running: &model.RunningState{}}}}},
		},
	})
	if report.Status != "unknown" || report.Confidence != "low" {
		t.Fatalf("unexpected incomplete report: %#v", report)
	}
	for _, context := range []string{"events", "container logs"} {
		if !contains(report.MissingContext, context) {
			t.Errorf("missing context does not contain %q: %#v", context, report.MissingContext)
		}
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

func findReason(report model.Report, code string) *model.Reason {
	for i := range report.Reasons {
		if report.Reasons[i].Code == code {
			return &report.Reasons[i]
		}
	}
	return nil
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
