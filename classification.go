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
)

type classifier interface {
	Classify(ctx context.Context, documents []classifiedInput, existingCategories []categoryCandidate) (classificationReport, error)
}

type mapleChatCompleter interface {
	ChatCompletion(ctx context.Context, reqBody chatRequest) (string, error)
}

type classifiedInput struct {
	ID       string
	Filename string
	Content  string
}

type classificationReport struct {
	Documents []classifiedDocument `json:"documents"`
	Summary   json.RawMessage      `json:"summary"`
}

type classifiedDocument struct {
	ID           string                `json:"id"`
	Topic        classifiedCategory    `json:"topic"`
	DocumentType classifiedCategory    `json:"document_type"`
	Sensitivity  classifiedSensitivity `json:"sensitivity"`
	Rationale    string                `json:"rationale"`
	Claims       []classifiedClaim     `json:"claims"`
}

type classifiedCategory struct {
	ID          int     `json:"id,omitempty"`
	Title       string  `json:"title,omitempty"`
	Description string  `json:"description,omitempty"`
	Category    string  `json:"category"`
	Confidence  float64 `json:"confidence"`
}

type categoryCandidate struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

type classifiedSensitivity struct {
	Level      int     `json:"level"`
	Label      string  `json:"label"`
	Confidence float64 `json:"confidence"`
}

type classifiedClaim struct {
	ID          string                `json:"id"`
	Claim       string                `json:"claim"`
	Category    string                `json:"category"`
	Confidence  float64               `json:"confidence"`
	Evidence    string                `json:"evidence"`
	Validation  classifiedValidation  `json:"validation"`
	Sensitivity classifiedSensitivity `json:"sensitivity"`
	Flags       []string              `json:"flags"`
}

type classifiedValidation struct {
	Status     string  `json:"status"`
	Confidence float64 `json:"confidence"`
	Rationale  string  `json:"rationale"`
}

type mapleClassifier struct {
	model  string
	client mapleChatCompleter
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

func (c mapleClassifier) Classify(ctx context.Context, documents []classifiedInput, existingCategories []categoryCandidate) (classificationReport, error) {
	if c.client == nil {
		return classificationReport{}, errors.New("maple client is required")
	}

	payload := make([]struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	}, 0, len(documents))
	for _, document := range documents {
		payload = append(payload, struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		}{ID: document.ID, Text: document.Content})
	}

	documentJSON, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return classificationReport{}, err
	}
	categoryJSON, err := json.MarshalIndent(existingCategories, "", "  ")
	if err != nil {
		return classificationReport{}, err
	}

	content, err := c.client.ChatCompletion(ctx, buildClassificationRequest(c.model, string(documentJSON), string(categoryJSON)))
	if err != nil {
		return classificationReport{}, err
	}
	report, err := parseClassificationContent(content)
	if err == nil {
		return report, nil
	}

	repairedContent, repairErr := c.client.ChatCompletion(ctx, buildRepairRequest(c.model, content))
	if repairErr != nil {
		return classificationReport{}, fmt.Errorf("%w; repair request failed: %v", err, repairErr)
	}
	report, repairErr = parseClassificationContent(repairedContent)
	if repairErr != nil {
		return classificationReport{}, fmt.Errorf("%w; repair also failed: %v", err, repairErr)
	}
	return report, nil
}

func buildClassificationRequest(model, documentJSON, categoryJSON string) chatRequest {
	return chatRequest{
		Model:          model,
		Temperature:    0,
		ResponseFormat: responseFormat{Type: "json_object"},
		Messages: []chatMessage{
			{
				Role: "system",
				Content: "Extract factual claims from each document, assign each document to one reusable category, and validate each claim against the source document. " +
					"Return one strict JSON object with a documents array. When an existing category fits, set topic.id to its integer id. " +
					"When no existing category fits, set topic.id to 0 and provide a concise topic.title and topic.description.",
			},
			{
				Role:    "user",
				Content: "Existing categories:\n" + categoryJSON + "\n\nDocuments:\n" + documentJSON,
			},
		},
	}
}

func buildRepairRequest(model, invalidContent string) chatRequest {
	return chatRequest{
		Model:          model,
		Temperature:    0,
		ResponseFormat: responseFormat{Type: "json_object"},
		Messages: []chatMessage{
			{
				Role:    "system",
				Content: "Repair malformed JSON into strict valid JSON. Return only the corrected JSON object.",
			},
			{
				Role:    "user",
				Content: invalidContent,
			},
		},
	}
}

func (c mapleClient) ChatCompletion(ctx context.Context, reqBody chatRequest) (string, error) {
	if c.apiKey == "" {
		return "", errors.New("MAPLE_API_KEY is required")
	}
	if c.httpClient == nil {
		c.httpClient = &http.Client{Timeout: 60 * time.Second}
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

func parseClassificationContent(content string) (classificationReport, error) {
	if !json.Valid([]byte(content)) {
		return classificationReport{}, fmt.Errorf("model returned non-JSON content: %s", content)
	}

	var report classificationReport
	if err := json.Unmarshal([]byte(content), &report); err != nil {
		return classificationReport{}, fmt.Errorf("decode classification report: %w", err)
	}
	if len(report.Documents) == 0 {
		return classificationReport{}, errors.New("classification report contained no documents")
	}
	return report, nil
}
