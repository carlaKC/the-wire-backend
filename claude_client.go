package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultClaudeBaseURL   = "https://api.anthropic.com"
	defaultClaudeModel     = "claude-sonnet-4-6"
	defaultClaudeMaxTokens = 16384
	anthropicAPIVersion    = "2023-06-01"
)

// claudeClient implements mapleChatCompleter against Anthropic's Messages API.
// It translates the OpenAI-style chatRequest the classifier emits into the
// Messages-API shape: system messages collapse into a top-level `system`
// string, and the response's content blocks are joined into one text string
// the existing parsers can consume.
type claudeClient struct {
	baseURL    string
	apiKey     string
	model      string
	maxTokens  int
	httpClient *http.Client
}

type claudeRequest struct {
	Model       string          `json:"model"`
	MaxTokens   int             `json:"max_tokens"`
	System      string          `json:"system,omitempty"`
	Messages    []claudeMessage `json:"messages"`
	Temperature float64         `json:"temperature"`
}

type claudeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type claudeResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Error      *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (c claudeClient) ChatCompletion(ctx context.Context, reqBody chatRequest) (string, error) {
	if c.apiKey == "" {
		return "", errors.New("ANTHROPIC_API_KEY is required")
	}
	if c.httpClient == nil {
		c.httpClient = &http.Client{Timeout: defaultMapleTimeout}
	}

	system, messages := splitSystemMessages(reqBody.Messages)
	model := reqBody.Model
	if c.model != "" {
		model = c.model
	}
	maxTokens := c.maxTokens
	if maxTokens <= 0 {
		maxTokens = defaultClaudeMaxTokens
	}

	body, err := json.Marshal(claudeRequest{
		Model:       model,
		MaxTokens:   maxTokens,
		System:      system,
		Messages:    messages,
		Temperature: reqBody.Temperature,
	})
	if err != nil {
		return "", err
	}

	endpoint := strings.TrimRight(c.baseURL, "/") + "/v1/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicAPIVersion)
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer httpResp.Body.Close()

	respBytes, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return "", err
	}
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return "", fmt.Errorf("claude api returned %s: %s", httpResp.Status, strings.TrimSpace(string(respBytes)))
	}

	var resp claudeResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return "", fmt.Errorf("decode claude response: %w", err)
	}
	if resp.Error != nil {
		return "", fmt.Errorf("claude error %s: %s", resp.Error.Type, resp.Error.Message)
	}
	if len(resp.Content) == 0 {
		return "", errors.New("claude response had no content blocks")
	}

	var out strings.Builder
	for _, block := range resp.Content {
		if block.Type == "text" {
			out.WriteString(block.Text)
		}
	}
	if resp.StopReason == "max_tokens" {
		return "", fmt.Errorf("claude response hit max_tokens (%d) and was truncated", maxTokens)
	}
	return strings.TrimSpace(out.String()), nil
}

// splitSystemMessages pulls every role:"system" entry out of msgs and joins
// their content with blank lines into a single system string. Anthropic's
// Messages API takes system as a top-level field, not as a message turn.
func splitSystemMessages(msgs []chatMessage) (string, []claudeMessage) {
	var systemParts []string
	out := make([]claudeMessage, 0, len(msgs))
	for _, m := range msgs {
		if m.Role == "system" {
			systemParts = append(systemParts, m.Content)
			continue
		}
		out = append(out, claudeMessage{Role: m.Role, Content: m.Content})
	}
	return strings.Join(systemParts, "\n\n"), out
}

func claudeMaxTokensFromEnv() int {
	raw := envOrDefault("CLAUDE_MAX_TOKENS", "")
	if raw == "" {
		return defaultClaudeMaxTokens
	}
	var n int
	if _, err := fmt.Sscanf(raw, "%d", &n); err != nil || n <= 0 {
		return defaultClaudeMaxTokens
	}
	return n
}

// claudeTimeoutFromEnv mirrors mapleTimeoutFromEnv: classification responses
// can take minutes, so the timeout is generous and overridable via
// CLAUDE_TIMEOUT_SECONDS.
func claudeTimeoutFromEnv() time.Duration {
	raw := envOrDefault("CLAUDE_TIMEOUT_SECONDS", "")
	if raw == "" {
		return defaultMapleTimeout
	}
	var n int
	if _, err := fmt.Sscanf(raw, "%d", &n); err != nil || n <= 0 {
		return defaultMapleTimeout
	}
	return time.Duration(n) * time.Second
}
