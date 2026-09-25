package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func testConfig(baseURL string) Config {
	return Config{
		BaseURL: baseURL,
		APIKey:  "test-key",
		Model:   "test-model",
	}
}

func TestChatSuccess(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %q, want /chat/completions", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"The pod is crash-looping because the database is unreachable."}}]}`))
	}))
	defer server.Close()

	client := NewClient(testConfig(server.URL))
	reply, err := client.Chat(context.Background(), []Message{{Role: "user", Content: "explain"}})
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}
	if reply != "The pod is crash-looping because the database is unreachable." {
		t.Errorf("reply = %q", reply)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer test-key")
	}
}

func TestChatSendsModelAndMessages(t *testing.T) {
	var body chatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	client := NewClient(testConfig(server.URL))
	_, err := client.Chat(context.Background(), []Message{
		{Role: "system", Content: "be helpful"},
		{Role: "user", Content: "hi"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if body.Model != "test-model" {
		t.Errorf("model = %q, want test-model", body.Model)
	}
	if len(body.Messages) != 2 {
		t.Errorf("messages = %d, want 2", len(body.Messages))
	}
}

func TestChatAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"Invalid API key","type":"invalid_request_error"}}`))
	}))
	defer server.Close()

	client := NewClient(testConfig(server.URL))
	_, err := client.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}})
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
	if !contains(err.Error(), "Invalid API key") {
		t.Errorf("error should include API message, got: %v", err)
	}
}

func TestChatRateLimitError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"Rate limit exceeded","type":"rate_limit_error"}}`))
	}))
	defer server.Close()

	client := NewClient(testConfig(server.URL))
	_, err := client.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}})
	if err == nil {
		t.Fatal("expected error for 429 response")
	}
}

func TestChatInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`not json at all`))
	}))
	defer server.Close()

	client := NewClient(testConfig(server.URL))
	_, err := client.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestChatEmptyChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"choices":[]}`))
	}))
	defer server.Close()

	client := NewClient(testConfig(server.URL))
	_, err := client.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}})
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
}

func TestChatContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.Write([]byte(`{"choices":[{"message":{"content":"late"}}]}`))
	}))
	defer server.Close()

	client := NewClient(testConfig(server.URL))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := client.Chat(ctx, []Message{{Role: "user", Content: "hi"}})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestChatMissingAPIKey(t *testing.T) {
	client := NewClient(Config{BaseURL: "http://localhost", Model: "m"})
	_, err := client.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}})
	if err == nil {
		t.Fatal("expected error for missing API key")
	}
}

func TestChatServerUnreachable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := server.URL
	server.Close()

	client := NewClient(testConfig(url))
	_, err := client.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}})
	if err == nil {
		t.Fatal("expected error for unreachable server")
	}
}

func TestConfigFromEnvDefaults(t *testing.T) {
	t.Setenv("KUBEWHY_LLM_BASE_URL", "")
	t.Setenv("KUBEWHY_LLM_API_KEY", "")
	t.Setenv("KUBEWHY_LLM_MODEL", "")

	config := ConfigFromEnv()
	if config.BaseURL != defaultBaseURL {
		t.Errorf("BaseURL = %q, want %q", config.BaseURL, defaultBaseURL)
	}
	if config.Model != defaultModel {
		t.Errorf("Model = %q, want %q", config.Model, defaultModel)
	}
	if config.APIKey != "" {
		t.Errorf("APIKey = %q, want empty", config.APIKey)
	}
}

func TestConfigFromEnvOverrides(t *testing.T) {
	t.Setenv("KUBEWHY_LLM_BASE_URL", "http://localhost:11434/v1/")
	t.Setenv("KUBEWHY_LLM_API_KEY", "secret")
	t.Setenv("KUBEWHY_LLM_MODEL", "llama3")

	config := ConfigFromEnv()
	if config.BaseURL != "http://localhost:11434/v1" {
		t.Errorf("BaseURL = %q, want trailing slash trimmed", config.BaseURL)
	}
	if config.APIKey != "secret" {
		t.Errorf("APIKey = %q, want secret", config.APIKey)
	}
	if config.Model != "llama3" {
		t.Errorf("Model = %q, want llama3", config.Model)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Errorf("truncate(short, 10) = %q", got)
	}
	if got := truncate("0123456789abcdef", 10); got != "0123456789..." {
		t.Errorf("truncate(long, 10) = %q", got)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || indexOf(s, substr) >= 0)
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
