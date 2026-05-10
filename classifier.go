package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// classifier is the model-backed contract used by the case-processing
// pipeline:
//
//   - Classify runs the case-level pipeline: one model call across the full
//     document set returning topic assignments and per-document metadata.
//   - ClassifyDocument runs the per-document heuristics pipeline: one model
//     call per document returning four fixed heuristics plus a list of
//     externally verifiable facts extracted from the document.
//   - ClassifyGroup runs the group heuristics pipeline: one model call for
//     every topic group that has enough unfiltered documents to compare.
//
// processCase invokes them in that order so the group scan can use both topic
// assignment and per-document filter decisions.
type classifier interface {
	Classify(ctx context.Context, documents []classifiedInput, existingTopics []topicCandidate) (classificationReport, error)
	ClassifyDocument(ctx context.Context, document classifiedInput) (documentClassification, error)
	ClassifyGroup(ctx context.Context, documents []classifiedInput, topicTitle string) ([]heuristic, error)
}

// documentClassification bundles the two outputs of the per-document
// classification pass: the closed set of heuristics and a short list of
// the most pertinent externally verifiable facts in the document.
type documentClassification struct {
	Heuristics    []heuristic
	FactsToVerify []string
}

// classifiedInput is the trimmed document the classifier sees: a stable per-
// case ID, the original filename for display, and the body text.
type classifiedInput struct {
	ID       string
	Filename string
	Content  string
}

// classificationReport is the shape the case-classification prompt returns.
// Keep the JSON tags in sync with prompts/case_classification_system.txt.
type classificationReport struct {
	Documents []classifiedDocument `json:"documents"`
	Summary   json.RawMessage      `json:"summary"`
}

type classifiedDocument struct {
	ID           string                `json:"id"`
	Topic        classifiedTopic       `json:"topic"`
	DocumentType classifiedTopic       `json:"document_type"`
	Sensitivity  classifiedSensitivity `json:"sensitivity"`
	Rationale    string                `json:"rationale"`
	Claims       []classifiedClaim     `json:"claims"`
}

type classifiedTopic struct {
	ID          int                  `json:"id,omitempty"`
	Title       string               `json:"title,omitempty"`
	Description string               `json:"description,omitempty"`
	Topic       string               `json:"topic"`
	Confidence  float64              `json:"confidence"`
	Importance  classifiedImportance `json:"importance,omitempty"`
}

type topicCandidate struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

type classifiedSensitivity struct {
	Level      int     `json:"level"`
	Label      string  `json:"label"`
	Confidence float64 `json:"confidence"`
}

type classifiedImportance struct {
	Score       string `json:"score"`
	Explanation string `json:"explanation"`
}

type classifiedClaim struct {
	ID          string                `json:"id"`
	Claim       string                `json:"claim"`
	Topic       string                `json:"topic"`
	Confidence  float64               `json:"confidence"`
	Evidence    string                `json:"evidence"`
	Validation  classifiedValidation  `json:"validation"`
	Sensitivity classifiedSensitivity `json:"sensitivity"`
	Flags       []string              `json:"flags"`
}

// UnmarshalJSON accepts either the full claim object or a bare string. Some
// model outputs collapse claims to plain strings; we accept both and lift the
// string into the Claim field.
func (c *classifiedClaim) UnmarshalJSON(data []byte) error {
	var claimText string
	if err := json.Unmarshal(data, &claimText); err == nil {
		c.Claim = claimText
		return nil
	}

	var raw struct {
		ID          flexibleString        `json:"id"`
		Claim       string                `json:"claim"`
		Topic       string                `json:"topic"`
		Confidence  float64               `json:"confidence"`
		Evidence    string                `json:"evidence"`
		Validation  classifiedValidation  `json:"validation"`
		Sensitivity classifiedSensitivity `json:"sensitivity"`
		Flags       []string              `json:"flags"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*c = classifiedClaim{
		ID:          string(raw.ID),
		Claim:       raw.Claim,
		Topic:       raw.Topic,
		Confidence:  raw.Confidence,
		Evidence:    raw.Evidence,
		Validation:  raw.Validation,
		Sensitivity: raw.Sensitivity,
		Flags:       raw.Flags,
	}
	return nil
}

type classifiedValidation struct {
	Status     string  `json:"status"`
	Confidence float64 `json:"confidence"`
	Rationale  string  `json:"rationale"`
}

// UnmarshalJSON accepts both the full validation object and a bare status
// string. Model repairs occasionally collapse validation to "supported" etc.
func (v *classifiedValidation) UnmarshalJSON(data []byte) error {
	var status string
	if err := json.Unmarshal(data, &status); err == nil {
		v.Status = status
		return nil
	}

	type classifiedValidationAlias classifiedValidation
	var validation classifiedValidationAlias
	if err := json.Unmarshal(data, &validation); err != nil {
		return err
	}
	*v = classifiedValidation(validation)
	return nil
}

type flexibleString string

func (s *flexibleString) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*s = flexibleString(text)
		return nil
	}

	var number json.Number
	if err := json.Unmarshal(data, &number); err == nil {
		*s = flexibleString(number.String())
		return nil
	}

	return fmt.Errorf("expected string or number")
}

// heuristicsReport is the shape the document-heuristics prompt returns. Keep
// the JSON tags in sync with prompts/document_heuristics_system.txt.
//
// FactsToVerify entries use llmFactToVerify because the prompt asks for plain
// strings but model output drift sometimes wraps them as objects (with "fact"
// or similar keys). The custom unmarshaler accepts both shapes.
type heuristicsReport struct {
	Heuristics    []llmDocumentHeuristic `json:"heuristics"`
	FactsToVerify []llmFactToVerify      `json:"facts_to_verify"`
}

type llmDocumentHeuristic struct {
	Name        string `json:"name"`
	Signal      string `json:"signal"`
	Score       string `json:"score"`
	Explanation string `json:"explanation"`
}

// llmFactToVerify is a tolerant wrapper around the per-document fact strings.
// The prompt asks for plain strings, but model output occasionally wraps each
// fact in an object — UnmarshalJSON accepts either shape.
type llmFactToVerify string

func (f *llmFactToVerify) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*f = llmFactToVerify(text)
		return nil
	}
	var wrapper struct {
		Fact      string `json:"fact"`
		Statement string `json:"statement"`
		Claim     string `json:"claim"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return err
	}
	switch {
	case wrapper.Fact != "":
		*f = llmFactToVerify(wrapper.Fact)
	case wrapper.Statement != "":
		*f = llmFactToVerify(wrapper.Statement)
	case wrapper.Claim != "":
		*f = llmFactToVerify(wrapper.Claim)
	default:
		*f = ""
	}
	return nil
}

type mapleClassifier struct {
	model  string
	client mapleChatCompleter
}

// Classify runs the case-level classification prompt. On a malformed JSON
// response it issues one schema-pinned repair pass before failing — the same
// retry shape used by ClassifyDocument.
func (c mapleClassifier) Classify(ctx context.Context, documents []classifiedInput, existingTopics []topicCandidate) (classificationReport, error) {
	if c.client == nil {
		return classificationReport{}, errors.New("maple client is required")
	}

	documentJSON, topicJSON, err := marshalCaseInputs(documents, existingTopics)
	if err != nil {
		return classificationReport{}, err
	}

	content, err := c.client.ChatCompletion(ctx, buildCaseClassificationRequest(c.model, topicJSON, documentJSON))
	if err != nil {
		return classificationReport{}, err
	}
	report, parseErr := parseClassificationContent(content)
	if parseErr == nil {
		return report, nil
	}
	if errors.Is(parseErr, errMapleQuotaExhausted) {
		return classificationReport{}, parseErr
	}

	repairedContent, repairErr := c.client.ChatCompletion(ctx, buildCaseClassificationRepairRequest(c.model, content))
	if repairErr != nil {
		return classificationReport{}, fmt.Errorf("%w; repair request failed: %v", parseErr, repairErr)
	}
	report, repairErr = parseClassificationContent(repairedContent)
	if repairErr != nil {
		if errors.Is(repairErr, errMapleQuotaExhausted) {
			return classificationReport{}, repairErr
		}
		return classificationReport{}, fmt.Errorf("%w; repair also failed: %v", parseErr, repairErr)
	}
	return report, nil
}

// ClassifyDocument runs the per-document heuristic prompt against a single
// document. Same retry shape as Classify. Returns both the closed set of
// heuristics and the open list of facts the model thinks an investigator
// could externally verify.
func (c mapleClassifier) ClassifyDocument(ctx context.Context, document classifiedInput) (documentClassification, error) {
	if c.client == nil {
		return documentClassification{}, errors.New("maple client is required")
	}

	content, err := c.client.ChatCompletion(ctx, buildDocumentHeuristicsRequest(c.model, document.Content))
	if err != nil {
		return documentClassification{}, err
	}
	classification, parseErr := parseDocumentHeuristics(content)
	if parseErr == nil {
		return classification, nil
	}

	repairedContent, repairErr := c.client.ChatCompletion(ctx, buildDocumentHeuristicsRepairRequest(c.model, content))
	if repairErr != nil {
		return documentClassification{}, fmt.Errorf("%w; repair request failed: %v", parseErr, repairErr)
	}
	classification, repairErr = parseDocumentHeuristics(repairedContent)
	if repairErr != nil {
		return documentClassification{}, fmt.Errorf("%w; repair also failed: %v", parseErr, repairErr)
	}
	return classification, nil
}

func marshalCaseInputs(documents []classifiedInput, existingTopics []topicCandidate) (documentJSON, topicJSON string, err error) {
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

	documentJSONBytes, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", "", err
	}
	topicJSONBytes, err := json.MarshalIndent(existingTopics, "", "  ")
	if err != nil {
		return "", "", err
	}
	return string(documentJSONBytes), string(topicJSONBytes), nil
}

// buildCaseClassificationRequest assembles the case-level prompt: the embedded
// system instructions plus a templated user message containing the existing
// topics and the documents to classify.
func buildCaseClassificationRequest(model, topicJSON, documentJSON string) chatRequest {
	return chatRequest{
		Model:          model,
		Temperature:    0,
		ResponseFormat: responseFormat{Type: "json_object"},
		Messages: []chatMessage{
			{Role: "system", Content: caseClassificationSystemPrompt},
			{Role: "user", Content: renderCaseClassificationUser(topicJSON, documentJSON)},
		},
	}
}

// buildCaseClassificationRepairRequest is the schema-pinned repair pass. The
// generic "fix this JSON" prompt has been observed to drift to other shapes,
// so the repair prompt restates the classification schema explicitly.
func buildCaseClassificationRepairRequest(model, invalidContent string) chatRequest {
	return chatRequest{
		Model:          model,
		Temperature:    0,
		ResponseFormat: responseFormat{Type: "json_object"},
		Messages: []chatMessage{
			{Role: "system", Content: caseClassificationRepairSystemPrompt},
			{Role: "user", Content: invalidContent},
		},
	}
}

// buildDocumentHeuristicsRequest assembles the per-document prompt: the
// embedded system instructions plus a templated user message containing the
// document body.
func buildDocumentHeuristicsRequest(model, content string) chatRequest {
	return chatRequest{
		Model:          model,
		Temperature:    0,
		ResponseFormat: responseFormat{Type: "json_object"},
		Messages: []chatMessage{
			{Role: "system", Content: documentHeuristicsSystemPrompt},
			{Role: "user", Content: renderDocumentHeuristicsUser(content)},
		},
	}
}

// buildDocumentHeuristicsRepairRequest is the schema-pinned repair pass for
// document heuristics. As with case classification, the generic repair prompt
// has been observed to drift to other shapes, so the repair prompt restates
// the heuristics schema explicitly.
func buildDocumentHeuristicsRepairRequest(model, invalidContent string) chatRequest {
	return chatRequest{
		Model:          model,
		Temperature:    0,
		ResponseFormat: responseFormat{Type: "json_object"},
		Messages: []chatMessage{
			{Role: "system", Content: documentHeuristicsRepairSystemPrompt},
			{Role: "user", Content: invalidContent},
		},
	}
}
