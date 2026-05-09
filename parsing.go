package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var errMapleQuotaExhausted = errors.New("maple quota exhausted: 电量不足，请充值后使用")

// invalidNumericIDPattern matches the case where the model emits a non-number
// where a numeric `id` is expected (e.g. translated text that the proxy
// substitutes in). The replacement coerces it to 0 so the schema parses.
var invalidNumericIDPattern = regexp.MustCompile(`("id"\s*:\s*)([^0-9"\{\[\]tfn\-\s][^,}\]\r\n]*)`)

// parseClassificationContent decodes a case-classification response. It
// tolerates the common "model returned prose around the JSON" failure mode by
// extracting the first JSON object.
func parseClassificationContent(content string) (classificationReport, error) {
	data, err := extractJSONContent(content)
	if err != nil {
		return classificationReport{}, err
	}

	var report classificationReport
	if err := json.Unmarshal(data, &report); err != nil {
		return classificationReport{}, fmt.Errorf("decode classification report: %w", err)
	}
	if len(report.Documents) == 0 {
		return classificationReport{}, errors.New("classification report contained no documents")
	}
	return report, nil
}

// parseDocumentHeuristics decodes a per-document heuristics response and maps
// it into the wire-shape heuristic (score → Rating, explanation → Description).
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

func extractJSONContent(content string) ([]byte, error) {
	if strings.Contains(content, "电量不足，请充值后使用") {
		return nil, errMapleQuotaExhausted
	}

	trimmed := strings.TrimSpace(content)
	start := strings.Index(trimmed, "{")
	if start < 0 {
		return nil, fmt.Errorf("model returned non-JSON content: %s", abbreviateContent(trimmed))
	}

	decoder := json.NewDecoder(strings.NewReader(trimmed[start:]))
	var raw json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		repaired := sanitizeInvalidNumericIDs(trimmed[start:])
		if repaired == trimmed[start:] {
			return nil, fmt.Errorf("model returned non-JSON content: %s", abbreviateContent(trimmed))
		}
		decoder = json.NewDecoder(strings.NewReader(repaired))
		if repairErr := decoder.Decode(&raw); repairErr != nil {
			return nil, fmt.Errorf("model returned non-JSON content: %s", abbreviateContent(trimmed))
		}
	}
	if len(raw) == 0 || raw[0] != '{' {
		return nil, fmt.Errorf("model returned non-object JSON content: %s", abbreviateContent(trimmed))
	}
	return raw, nil
}

func sanitizeInvalidNumericIDs(content string) string {
	return invalidNumericIDPattern.ReplaceAllString(content, `${1}0`)
}

func abbreviateContent(content string) string {
	const limit = 500
	if len(content) <= limit {
		return content
	}
	return content[:limit] + "...[truncated]"
}
