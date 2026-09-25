package integrations

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPagerDutyNotifySendsPayload(t *testing.T) {
	var received map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &received)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	n := &PagerDutyNotifier{RoutingKey: "test-key", EventsURL: server.URL, HTTPClient: server.Client()}
	if err := n.Notify(testReport()); err != nil {
		t.Fatal(err)
	}
}

func TestPagerDutyNotifyServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	n := &PagerDutyNotifier{RoutingKey: "test-key", EventsURL: server.URL, HTTPClient: server.Client()}
	if err := n.Notify(testReport()); err == nil {
		t.Fatal("expected error on 400")
	}
}

func TestMapStatusToSeverity(t *testing.T) {
	cases := map[string]string{"broken": "critical", "degraded": "warning", "unknown": "info", "healthy": "info"}
	for status, want := range cases {
		if got := mapStatusToSeverity(status); got != want {
			t.Errorf("mapStatusToSeverity(%q) = %q, want %q", status, got, want)
		}
	}
}
