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
	Signal      string `json:"signal"`
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

// buildDocumentHeuristicsRepairRequest is a schema-aware repair pass for the
// per-document heuristics response. The generic buildRepairRequest only says
// "fix this JSON" and has been observed to drift toward an unrelated schema
// (the classification report's documents/claims shape) when the model has
// no anchor for what valid output looks like. This version pins the schema
// explicitly so the repair stays on-topic.
func buildDocumentHeuristicsRepairRequest(model, invalidContent string) chatRequest {
	return chatRequest{
		Model:          model,
		Temperature:    0,
		ResponseFormat: responseFormat{Type: "json_object"},
		Messages: []chatMessage{
			{
				Role: "system",
				Content: `Repair malformed JSON into a strict JSON object that exactly matches this schema:

{
  "heuristics": [
    {"name": "<string>", "signal": "positive|negative", "score": "high|medium|low", "explanation": "<string>"}
  ]
}

The "heuristics" array MUST contain exactly four entries, with names: consistency, references, emotive_language, ideology_or_incentives. The "signal" field is fixed per name: consistency=positive, references=positive, emotive_language=negative, ideology_or_incentives=negative. Preserve every field's value as accurately as you can from the input — if a field's value is itself broken (missing quotes around a string, unquoted text), interpret it conservatively and quote it.

Return ONLY the corrected JSON object. No commentary, no markdown, no extra top-level fields.`,
			},
			{
				Role:    "user",
				Content: invalidContent,
			},
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

	repairedContent, repairErr := c.client.ChatCompletion(ctx, buildDocumentHeuristicsRepairRequest(c.model, content))
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
			Signal:      h.Signal,
			Rating:      h.Score,
			Description: h.Explanation,
		})
	}
	return out, nil
}
