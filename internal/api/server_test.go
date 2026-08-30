package api

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDiagnoseEndpoint(t *testing.T) {
	body := []byte(`{"pod":{"metadata":{"name":"api"},"status":{"phase":"Failed"}}}`)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: NewServer(nil, nil).Handler()}
	go server.Serve(listener)
	defer server.Close()
	client := &http.Client{Timeout: time.Second}
	response, err := client.Post("http://"+listener.Addr().String()+"/api/v1/diagnose", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	var report struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(response.Body).Decode(&report); err != nil {
		t.Fatal(err)
	}
	if report.Status != "broken" {
		t.Fatalf("status = %q", report.Status)
	}
}

func TestDiagnoseRejectsMissingPodName(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: NewServer(nil, nil).Handler()}
	go server.Serve(listener)
	defer server.Close()
	request, err := http.NewRequest(http.MethodPost, "http://"+listener.Addr().String()+"/api/v1/diagnose", bytes.NewBufferString(`{"pod":{"metadata":{}}}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", response.StatusCode)
	}
}

func TestDiagnoseRejectsTrailingJSON(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/diagnose", strings.NewReader(`{"pod":{"metadata":{"name":"api"}}} {}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	NewServer(nil, nil).Handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}

func TestDiagnoseRejectsOversizedBody(t *testing.T) {
	body := bytes.NewBufferString(`{"pod":{"metadata":{"name":"api"}},"logs":[{"container":"api","text":"`)
	_, _ = body.Write(bytes.Repeat([]byte{'x'}, int(maxRequestBytes)))
	_, _ = body.WriteString(`"}]}`)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/diagnose", body)
	response := httptest.NewRecorder()
	NewServer(nil, nil).Handler().ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestRequestIDPropagatesIncomingID(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	request.Header.Set("X-Request-ID", "incident-123")
	response := httptest.NewRecorder()
	NewServer(nil, nil).Handler().ServeHTTP(response, request)
	if got := response.Header().Get("X-Request-ID"); got != "incident-123" {
		t.Fatalf("X-Request-ID = %q, want incident-123", got)
	}
}

func TestRequestIDIsGeneratedWhenMissing(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()
	NewServer(nil, nil).Handler().ServeHTTP(response, request)
	if response.Header().Get("X-Request-ID") == "" {
		t.Fatal("expected generated X-Request-ID")
	}
}
