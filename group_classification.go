package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

//go:embed prompts/group_heuristics.txt
var groupHeuristicsPromptTemplate string

const (
	documentsTextPlaceholder = "{{DOCUMENTS_TEXT}}"
	topicTitlePlaceholder    = "{{TOPIC_TITLE}}"
)

func renderGroupHeuristicsPrompt(topicTitle, bundled string) string {
	out := strings.Replace(groupHeuristicsPromptTemplate, topicTitlePlaceholder, topicTitle, 1)
	return strings.Replace(out, documentsTextPlaceholder, bundled, 1)
}

func bundleDocumentsForGroupScan(docs []classifiedInput) string {
	var b strings.Builder
	for i, d := range docs {
		name := strings.TrimSpace(d.Filename)
		if name == "" {
			name = d.ID
		}
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("--- Document: ")
		b.WriteString(name)
		b.WriteString(" ---\n")
		b.WriteString(strings.TrimRight(d.Content, "\n"))
	}
	return b.String()
}

func buildGroupHeuristicsRequest(model, topicTitle, bundled string) chatRequest {
	return chatRequest{
		Model:          model,
		Temperature:    0,
		ResponseFormat: responseFormat{Type: "json_object"},
		Messages: []chatMessage{
			{Role: "user", Content: renderGroupHeuristicsPrompt(topicTitle, bundled)},
		},
	}
}

func buildGroupHeuristicsRepairRequest(model, invalidContent string) chatRequest {
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

The "heuristics" array must contain at least one entry. Preserve every field's value as accurately as you can from the input. If a field's value is itself broken, interpret it conservatively and quote it.

Return ONLY the corrected JSON object. No commentary, no markdown, no extra top-level fields.`,
			},
			{Role: "user", Content: invalidContent},
		},
	}
}

func (c mapleClassifier) ClassifyGroup(ctx context.Context, documents []classifiedInput, topicTitle string) ([]heuristic, error) {
	if c.client == nil {
		return nil, errors.New("maple client is required")
	}
	if len(documents) == 0 {
		return nil, errors.New("ClassifyGroup requires at least one document")
	}

	content, err := c.client.ChatCompletion(ctx, buildGroupHeuristicsRequest(c.model, topicTitle, bundleDocumentsForGroupScan(documents)))
	if err != nil {
		return nil, err
	}
	heuristics, parseErr := parseGroupHeuristics(content)
	if parseErr == nil {
		return heuristics, nil
	}

	repairedContent, repairErr := c.client.ChatCompletion(ctx, buildGroupHeuristicsRepairRequest(c.model, content))
	if repairErr != nil {
		return nil, fmt.Errorf("%w; repair request failed: %v", parseErr, repairErr)
	}
	heuristics, repairErr = parseGroupHeuristics(repairedContent)
	if repairErr != nil {
		return nil, fmt.Errorf("%w; repair also failed: %v", parseErr, repairErr)
	}
	return heuristics, nil
}

func parseGroupHeuristics(content string) ([]heuristic, error) {
	data, err := extractJSONContent(content)
	if err != nil {
		return nil, fmt.Errorf("decode group heuristics report: %w", err)
	}
	var report heuristicsReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("decode group heuristics report: %w", err)
	}
	if len(report.Heuristics) == 0 {
		return nil, errors.New("group heuristics report contained no entries")
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
