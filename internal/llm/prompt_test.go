package llm

import (
	"strings"
	"testing"
	"time"

	"github.com/kubewhy/kubewhy/internal/model"
)

func sampleReport() model.Report {
	return model.Report{
		GeneratedAt: time.Now(),
		Pod:         model.PodIdentity{Name: "api", Namespace: "payments", Node: "worker-1", Phase: "Running"},
		Status:      "broken",
		Confidence:  "high",
		Summary:     "Pod payments/api is broken: 2 reasons found.",
		RootCause: &model.Reason{
			Code:        "crash_loop",
			Severity:    "critical",
			Confidence:  "high",
			Title:       "Container is crash-looping",
			Explanation: "The api container terminated repeatedly.",
			Evidence:    []string{"waiting reason=CrashLoopBackOff", "restartCount=7"},
			Remediation: []string{"Inspect previous logs", "Fix the startup failure"},
		},
		Reasons: []model.Reason{
			{
				Code:     "crash_loop",
				Severity: "critical",
				Title:    "Container is crash-looping",
			},
			{
				Code:     "not_ready",
				Severity: "warning",
				Title:    "Container is not ready",
			},
		},
		Containers: []model.ContainerFinding{
			{Name: "api", Kind: "app", State: "waiting", Ready: false, RestartCount: 7, Details: []string{"CrashLoopBackOff"}},
		},
		RelevantEvents: []model.EventFinding{
			{Type: "Warning", Reason: "BackOff", Message: "Back-off restarting failed container", Count: 8, Severity: "warning"},
		},
	}
}

func TestBuildPromptHasTwoMessages(t *testing.T) {
	messages := BuildPrompt(sampleReport())
	if len(messages) != 2 {
		t.Fatalf("got %d messages, want 2", len(messages))
	}
	if messages[0].Role != "system" {
		t.Errorf("first message role = %q, want system", messages[0].Role)
	}
	if messages[1].Role != "user" {
		t.Errorf("second message role = %q, want user", messages[1].Role)
	}
}

func TestBuildPromptSystemAsksForKubectlCommands(t *testing.T) {
	messages := BuildPrompt(sampleReport())
	if !strings.Contains(messages[0].Content, "kubectl") {
		t.Error("system prompt should ask for kubectl commands")
	}
	if !strings.Contains(messages[0].Content, "plain") {
		t.Error("system prompt should ask for plain language")
	}
}

func TestBuildPromptIncludesStatus(t *testing.T) {
	messages := BuildPrompt(sampleReport())
	user := messages[1].Content
	if !strings.Contains(user, "broken") {
		t.Error("user content should include status")
	}
	if !strings.Contains(user, "payments/api") {
		t.Error("user content should include pod identity")
	}
}

func TestBuildPromptIncludesRootCause(t *testing.T) {
	messages := BuildPrompt(sampleReport())
	user := messages[1].Content
	if !strings.Contains(user, "Container is crash-looping") {
		t.Error("user content should include root cause title")
	}
	if !strings.Contains(user, "CrashLoopBackOff") {
		t.Error("user content should include evidence")
	}
	if !strings.Contains(user, "Inspect previous logs") {
		t.Error("user content should include remediation")
	}
}

func TestBuildPromptIncludesContainersAndEvents(t *testing.T) {
	messages := BuildPrompt(sampleReport())
	user := messages[1].Content
	if !strings.Contains(user, "Back-off restarting failed container") {
		t.Error("user content should include event message")
	}
	if !strings.Contains(user, "restarts=7") {
		t.Error("user content should include container finding")
	}
}

func TestBuildPromptIncludesMissingContextWarning(t *testing.T) {
	report := sampleReport()
	report.MissingContext = []string{"container logs", "events"}
	messages := BuildPrompt(report)
	if !strings.Contains(messages[1].Content, "container logs") {
		t.Error("user content should include missing context")
	}
}

func TestBuildPromptHandlesEmptyReport(t *testing.T) {
	messages := BuildPrompt(model.Report{Status: "unknown", Pod: model.PodIdentity{Name: "api"}})
	if len(messages) != 2 {
		t.Fatalf("got %d messages, want 2", len(messages))
	}
	if messages[1].Content == "" {
		t.Error("user content should not be empty")
	}
}
