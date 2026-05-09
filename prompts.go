package main

import (
	_ "embed"
	"strings"
)

// All prompt text lives in prompts/. Each prompt is split into a system
// message (model instructions, fixed) and, where applicable, a user message
// template (placeholders substituted at request time).

//go:embed prompts/case_classification_system.txt
var caseClassificationSystemPrompt string

//go:embed prompts/case_classification_user.txt
var caseClassificationUserTemplate string

//go:embed prompts/case_classification_repair_system.txt
var caseClassificationRepairSystemPrompt string

//go:embed prompts/document_heuristics_system.txt
var documentHeuristicsSystemPrompt string

//go:embed prompts/document_heuristics_user.txt
var documentHeuristicsUserTemplate string

//go:embed prompts/document_heuristics_repair_system.txt
var documentHeuristicsRepairSystemPrompt string

const (
	topicsPlaceholder       = "{{TOPICS}}"
	documentsPlaceholder    = "{{DOCUMENTS}}"
	documentTextPlaceholder = "{{DOCUMENT_TEXT}}"
)

func renderCaseClassificationUser(topicJSON, documentJSON string) string {
	out := strings.Replace(caseClassificationUserTemplate, topicsPlaceholder, topicJSON, 1)
	return strings.Replace(out, documentsPlaceholder, documentJSON, 1)
}

func renderDocumentHeuristicsUser(content string) string {
	return strings.Replace(documentHeuristicsUserTemplate, documentTextPlaceholder, content, 1)
}
