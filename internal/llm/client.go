package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	defaultBaseURL = "https://api.openai.com/v1"
	defaultModel   = "gpt-4o-mini"
	requestTimeout = 30 * time.Second
)

// Config holds the OpenAI-compatible endpoint configuration.
type Config struct {
	BaseURL string // e.g. https://api.openai.com/v1
	APIKey  string
	Model   string // e.g. gpt-4o-mini
}

// ConfigFromEnv reads KUBEWHY_LLM_BASE_URL, KUBEWHY_LLM_API_KEY, and KUBEWHY_LLM_MODEL.
func ConfigFromEnv() Config {
	config := Config{
		BaseURL: defaultBaseURL,
		Model:   defaultModel,
	}
	if url := os.Getenv("KUBEWHY_LLM_BASE_URL"); url != "" {
		config.BaseURL = strings.TrimRight(url, "/")
	}
	if key := os.Getenv("KUBEWHY_LLM_API_KEY"); key != "" {
		config.APIKey = key
	}
	if model := os.Getenv("KUBEWHY_LLM_MODEL"); model != "" {
		config.Model = model
	}
	return config
}

// Client speaks the OpenAI chat completions API.
type Client struct {
	config Config
	http   *http.Client
}

// NewClient creates a Client. If config.HTTPClient is nil a default with a
// 30-second timeout is used.
func NewClient(config Config) *Client {
	return &Client{
		config: config,
		http:   &http.Client{Timeout: requestTimeout},
	}
}

// Message is one chat message in the conversation.
type Message struct {
	Role    string `json:"role"` // "system", "user", or "assistant"
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

// Chat sends the messages and returns the assistant's reply.
func (c *Client) Chat(ctx context.Context, messages []Message) (string, error) {
	if c.config.APIKey == "" {
		return "", fmt.Errorf("llm: API key is required; set KUBEWHY_LLM_API_KEY or use --llm-key")
	}

	body, err := json.Marshal(chatRequest{
		Model:       c.config.Model,
		Messages:    messages,
		Temperature: 0.3,
	})
	if err != nil {
		return "", fmt.Errorf("llm: encode request: %w", err)
	}

	url := strings.TrimRight(c.config.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("llm: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.config.APIKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("llm: request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return "", fmt.Errorf("llm: read response: %w", err)
	}

	var chatResp chatResponse
	if err := json.Unmarshal(data, &chatResp); err != nil {
		return "", fmt.Errorf("llm: parse response (status %d): %w", resp.StatusCode, err)
	}
	if chatResp.Error != nil {
		return "", fmt.Errorf("llm: API error (status %d): %s", resp.StatusCode, chatResp.Error.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("llm: unexpected status %d: %s", resp.StatusCode, truncate(string(data), 200))
	}
	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("llm: response contains no choices")
	}
	return chatResp.Choices[0].Message.Content, nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
