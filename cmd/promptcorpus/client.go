package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// chatClient is the small subset of model interaction the corpus tool needs:
// send a list of role/content turns, get a single text reply.
type chatClient interface {
	ChatCompletion(ctx context.Context, req chatRequest) (string, error)
	label() string
}

type chatRequest struct {
	Messages       []chatMessage  `json:"messages"`
	Temperature    float64        `json:"temperature"`
	ResponseFormat responseFormat `json:"response_format,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type responseFormat struct {
	Type string `json:"type"`
}

// claudeClient mirrors backend/claude_client.go but is intentionally
// self-contained — duplication keeps the corpus tool independent of the
// server package while still exercising the same wire shape and headers.
type claudeClient struct {
	baseURL   string
	apiKey    string
	model     string
	maxTokens int
	timeout   time.Duration
	hc        *http.Client
}

func (c *claudeClient) label() string { return "claude:" + c.model }

func (c *claudeClient) ChatCompletion(ctx context.Context, req chatRequest) (string, error) {
	if c.hc == nil {
		c.hc = &http.Client{Timeout: c.timeout}
	}

	type clMsg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	var systemParts []string
	out := make([]clMsg, 0, len(req.Messages))
	for _, m := range req.Messages {
		if m.Role == "system" {
			systemParts = append(systemParts, m.Content)
			continue
		}
		out = append(out, clMsg{Role: m.Role, Content: m.Content})
	}

	body, err := json.Marshal(struct {
		Model       string  `json:"model"`
		MaxTokens   int     `json:"max_tokens"`
		System      string  `json:"system,omitempty"`
		Messages    []clMsg `json:"messages"`
		Temperature float64 `json:"temperature"`
	}{
		Model:       c.model,
		MaxTokens:   c.maxTokens,
		System:      strings.Join(systemParts, "\n\n"),
		Messages:    out,
		Temperature: req.Temperature,
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
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.hc.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("claude api %s: %s", resp.Status, strings.TrimSpace(string(respBytes)))
	}

	var parsed struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
	}
	if err := json.Unmarshal(respBytes, &parsed); err != nil {
		return "", fmt.Errorf("decode claude response: %w", err)
	}
	if len(parsed.Content) == 0 {
		return "", errors.New("claude response had no content blocks")
	}
	if parsed.StopReason == "max_tokens" {
		return "", fmt.Errorf("claude response hit max_tokens (%d)", c.maxTokens)
	}
	var b strings.Builder
	for _, blk := range parsed.Content {
		if blk.Type == "text" {
			b.WriteString(blk.Text)
		}
	}
	return strings.TrimSpace(b.String()), nil
}

// mapleClient calls the Maple proxy's OpenAI-shaped chat completions endpoint.
type mapleClient struct {
	baseURL string
	apiKey  string
	model   string
	timeout time.Duration
	hc      *http.Client
}

func (c *mapleClient) label() string { return "maple:" + c.model }

func (c *mapleClient) ChatCompletion(ctx context.Context, req chatRequest) (string, error) {
	if c.hc == nil {
		c.hc = &http.Client{Timeout: c.timeout}
	}
	body, err := json.Marshal(struct {
		Model          string         `json:"model"`
		Messages       []chatMessage  `json:"messages"`
		Temperature    float64        `json:"temperature"`
		ResponseFormat responseFormat `json:"response_format,omitempty"`
	}{
		Model:          c.model,
		Messages:       req.Messages,
		Temperature:    req.Temperature,
		ResponseFormat: req.ResponseFormat,
	})
	if err != nil {
		return "", err
	}
	endpoint := strings.TrimRight(c.baseURL, "/") + "/v1/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.hc.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("maple proxy %s: %s", resp.Status, strings.TrimSpace(string(respBytes)))
	}
	var parsed struct {
		Choices []struct {
			Message chatMessage `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBytes, &parsed); err != nil {
		return "", fmt.Errorf("decode maple response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", errors.New("maple response had no choices")
	}
	return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
}

// opencodeClient shells out to the opencode CLI and reads stdout.
type opencodeClient struct {
	bin     string
	model   string
	timeout time.Duration
}

func (c *opencodeClient) label() string { return "opencode:" + c.model }

func (c *opencodeClient) ChatCompletion(ctx context.Context, req chatRequest) (string, error) {
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}
	prompt := renderOpencodePrompt(req)
	cmd := exec.CommandContext(ctx, c.bin, "run", "-m", c.model, prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", fmt.Errorf("opencode run failed: %w (stderr=%q)", err, tail(stderr.String(), 1024))
	}
	content := strings.TrimSpace(stdout.String())
	if content == "" {
		return "", fmt.Errorf("opencode run returned empty output (stderr=%q)", tail(stderr.String(), 1024))
	}
	return content, nil
}

func renderOpencodePrompt(req chatRequest) string {
	var b strings.Builder
	if req.ResponseFormat.Type == "json_object" {
		b.WriteString("Return only valid JSON. Do not wrap it in Markdown fences or add explanatory prose.\n\n")
	}
	for _, m := range req.Messages {
		role := strings.TrimSpace(m.Role)
		if role == "" {
			role = "message"
		}
		b.WriteString(strings.ToUpper(role))
		b.WriteString(":\n")
		b.WriteString(strings.TrimSpace(m.Content))
		b.WriteString("\n\n")
	}
	return strings.TrimSpace(b.String())
}

func tail(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return "..." + s[len(s)-max:]
}
