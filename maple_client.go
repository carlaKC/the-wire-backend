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
	defaultMapleBaseURL = "http://127.0.0.1:8081"
	defaultMapleModel   = "deepseek-v4-pro"
	// defaultMapleTimeout is generous on purpose: case classification across
	// multi-document submissions routinely takes 1–3 minutes server-side, and
	// the previous 60s value was tripping legitimate calls. Override at
	// startup via MAPLE_TIMEOUT_SECONDS.
	defaultMapleTimeout = 5 * time.Minute
)

// mapleChatCompleter is the dependency the classifier needs from the model
// backend. It exists as an interface so tests can inject a recording fake
// instead of a real HTTP client.
type mapleChatCompleter interface {
	ChatCompletion(ctx context.Context, reqBody chatRequest) (string, error)
}

type mapleClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

type chatRequest struct {
	Model          string         `json:"model"`
	Messages       []chatMessage  `json:"messages"`
	Temperature    float64        `json:"temperature"`
	ResponseFormat responseFormat `json:"response_format,omitempty"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

func (c mapleClient) ChatCompletion(ctx context.Context, reqBody chatRequest) (string, error) {
	if c.apiKey == "" {
		return "", errors.New("MAPLE_API_KEY is required")
	}
	if c.httpClient == nil {
		c.httpClient = &http.Client{Timeout: defaultMapleTimeout}
	}

	body, err := json.Marshal(reqBody)
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
		return "", fmt.Errorf("maple proxy returned %s: %s", httpResp.Status, strings.TrimSpace(string(respBytes)))
	}

	var resp chatResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return "", fmt.Errorf("decode chat response: %w", err)
	}
	if resp.Error != nil {
		return "", fmt.Errorf("maple error %s: %s", resp.Error.Type, resp.Error.Message)
	}
	if len(resp.Choices) == 0 {
		return "", errors.New("maple response had no choices")
	}

	return strings.TrimSpace(resp.Choices[0].Message.Content), nil
}
