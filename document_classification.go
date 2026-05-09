package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

//go:embed prompts/document_heuristics.txt
var documentHeuristicsPromptTemplate string

const documentTextPlaceholder = "{{DOCUMENT_TEXT}}"

// renderDocumentHeuristicsPrompt substitutes the document content into the
// per-document heuristic prompt template. The result is the full prompt
// string to send to the model.
func renderDocumentHeuristicsPrompt(content string) string {
	return strings.Replace(documentHeuristicsPromptTemplate, documentTextPlaceholder, content, 1)
}

// heuristicsReport mirrors the JSON schema the prompt asks the LLM to
// produce. Keep this in sync with prompts/document_heuristics.txt.
type heuristicsReport struct {
	Heuristics []llmDocumentHeuristic `json:"heuristics"`
}

type llmDocumentHeuristic struct {
	Name        string `json:"name"`
	Score       string `json:"score"`
	Explanation string `json:"explanation"`
}

func buildDocumentHeuristicsRequest(model, content string) chatRequest {
	return chatRequest{
		Model:          model,
		Temperature:    0,
		ResponseFormat: responseFormat{Type: "json_object"},
		Messages: []chatMessage{
			{Role: "user", Content: renderDocumentHeuristicsPrompt(content)},
		},
	}
}

// ClassifyDocument runs the per-document heuristic prompt against a single
// document and returns the parsed heuristics. On a malformed JSON response
// it issues one repair pass — same retry pattern as Classify — before
// failing.
func (c mapleClassifier) ClassifyDocument(ctx context.Context, document classifiedInput) ([]heuristic, error) {
	if c.client == nil {
		return nil, errors.New("maple client is required")
	}

	content, err := c.client.ChatCompletion(ctx, buildDocumentHeuristicsRequest(c.model, document.Content))
	if err != nil {
		return nil, err
	}
	heuristics, parseErr := parseDocumentHeuristics(content)
	if parseErr == nil {
		return heuristics, nil
	}

	repairedContent, repairErr := c.client.ChatCompletion(ctx, buildRepairRequest(c.model, content))
	if repairErr != nil {
		return nil, fmt.Errorf("%w; repair request failed: %v", parseErr, repairErr)
	}
	heuristics, repairErr = parseDocumentHeuristics(repairedContent)
	if repairErr != nil {
		return nil, fmt.Errorf("%w; repair also failed: %v", parseErr, repairErr)
	}
	return heuristics, nil
}

// parseDocumentHeuristics decodes the LLM's response into wire-shape
// heuristics, mapping score → rating and explanation → description.
func parseDocumentHeuristics(content string) ([]heuristic, error) {
	if !json.Valid([]byte(content)) {
		return nil, fmt.Errorf("model returned non-JSON content: %s", content)
	}
	var report heuristicsReport
	if err := json.Unmarshal([]byte(content), &report); err != nil {
		return nil, fmt.Errorf("decode heuristics report: %w", err)
	}
	if len(report.Heuristics) == 0 {
		return nil, errors.New("heuristics report contained no entries")
	}
	out := make([]heuristic, 0, len(report.Heuristics))
	for _, h := range report.Heuristics {
		out = append(out, heuristic{
			Name:        h.Name,
			Rating:      h.Score,
			Description: h.Explanation,
		})
	}
	return out, nil
}
