package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	var report heuristicsReport
	decoder := json.NewDecoder(strings.NewReader(content))
	if err := decoder.Decode(&report); err != nil {
		return nil, fmt.Errorf("decode heuristics report: %w; %s", err, describeJSONFailure(content, err))
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
		return nil, fmt.Errorf("model returned content with no JSON object: %s", abbreviateContent(trimmed))
	}

	candidate := trimmed[start:]
	raw, decodeErr := decodeFirstJSONObject(candidate)
	if decodeErr == nil {
		return raw, nil
	}

	// Some failures are recoverable: the proxy occasionally emits a non-numeric
	// token where a numeric id is expected, which the sanitizer coerces to 0.
	// We only swap to the sanitized content if it actually changed something.
	repaired := sanitizeInvalidNumericIDs(candidate)
	if repaired != candidate {
		if rawAfterRepair, err := decodeFirstJSONObject(repaired); err == nil {
			return rawAfterRepair, nil
		}
	}

	return nil, fmt.Errorf("model returned invalid JSON: %w; %s", decodeErr, describeJSONFailure(candidate, decodeErr))
}

// decodeFirstJSONObject decodes one JSON value from s and verifies it's an
// object. The caller may pass content with trailing prose; the decoder reads
// up to the end of the first value and ignores the rest.
func decodeFirstJSONObject(s string) (json.RawMessage, error) {
	decoder := json.NewDecoder(strings.NewReader(s))
	var raw json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 || raw[0] != '{' {
		return nil, fmt.Errorf("expected JSON object")
	}
	return raw, nil
}

// describeJSONFailure produces a diagnostic string showing the part of content
// most relevant to the decode error. The single most useful piece of evidence
// is the failure mode and where in the byte stream it occurred:
//
//   - *json.SyntaxError gives a byte Offset; show a window around it. Real
//     classification responses are kilobytes long, so the failure is almost
//     never in the first 500 bytes.
//   - io.ErrUnexpectedEOF / io.EOF means the JSON is incomplete — usually
//     because an upstream max-token cap chopped the response. Show the tail
//     and flag truncation.
//   - Other errors (rare): fall back to the head excerpt.
//
// The total content size is always reported. A full but invalid response and a
// truncated valid response have very different size signatures.
func describeJSONFailure(s string, err error) string {
	size := len(s)
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		return fmt.Sprintf("[%d bytes] near offset %d: %s", size, syntaxErr.Offset, jsonWindow(s, int(syntaxErr.Offset), 200))
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return fmt.Sprintf("[%d bytes, response appears truncated] tail: %s", size, jsonTail(s, 400))
	}
	return fmt.Sprintf("[%d bytes] head: %s", size, abbreviateContent(s))
}

func jsonWindow(s string, offset, window int) string {
	start := offset - window
	if start < 0 {
		start = 0
	}
	end := offset + window
	if end > len(s) {
		end = len(s)
	}
	if offset > len(s) {
		offset = len(s)
	}
	if offset < start {
		offset = start
	}
	return s[start:offset] + "<<HERE>>" + s[offset:end]
}

func jsonTail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
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
