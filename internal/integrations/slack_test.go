package integrations

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kubewhy/kubewhy/internal/model"
)

func testReport() model.Report {
	rootCause := model.Reason{
		Code: "oom_killed", Severity: "critical", Confidence: "high",
		Title: "Container was killed for exceeding memory",
		Explanation: "The previous container process exceeded its memory limit.",
		Evidence: []string{"container=api", "last termination reason=OOMKilled"},
		Remediation: []string{"Increase the memory limit"},
	}
	return model.Report{
		GeneratedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Pod:         model.PodIdentity{Name: "api", Namespace: "payments"},
		Status:      "broken", Confidence: "high",
		Summary:   "Pod payments/api is broken: 1 reason found.",
		RootCause: &rootCause,
		Reasons:   []model.Reason{rootCause},
	}
}

func TestSlackNotifySendsPayload(t *testing.T) {
	var received map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &received)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	n := NewSlackNotifier(server.URL)
	if err := n.Notify(testReport()); err != nil {
		t.Fatal(err)
	}
	blocks, ok := received["blocks"].([]interface{})
	if !ok || len(blocks) == 0 {
		t.Fatal("expected blocks in payload")
	}
}

func TestSlackNotifyServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	n := NewSlackNotifier(server.URL)
	if err := n.Notify(testReport()); err == nil {
		t.Fatal("expected error on 500")
	}
}
