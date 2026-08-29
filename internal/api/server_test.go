package api

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
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
