package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// answerKey is one entry in the corpus file. Each heuristic carries the
// expected rating ("high"|"medium"|"low") and a free-text explanation
// describing why the reviewer expects that value. The explanation is what
// gets passed to the improver meta-prompt when the model's actual output
// disagrees with the expected value.
type answerKey struct {
	Document        string            `json:"document"`
	Consistency     expectedHeuristic `json:"consistency"`
	References      expectedHeuristic `json:"references"`
	EmotiveLanguage expectedHeuristic `json:"emotive_language"`
	Ideology        expectedHeuristic `json:"ideology"`
}

type expectedHeuristic struct {
	Expected    string `json:"expected"`
	Explanation string `json:"explanation"`
}

// expectations exposes the four heuristics in a stable order for iteration.
// Returning a slice keeps the comparison logic uniform — callers don't need
// to special-case each named field.
func (k answerKey) expectations() []namedExpectation {
	return []namedExpectation{
		{Name: "consistency", Expected: k.Consistency},
		{Name: "references", Expected: k.References},
		{Name: "emotive_language", Expected: k.EmotiveLanguage},
		{Name: "ideology", Expected: k.Ideology},
	}
}

type namedExpectation struct {
	Name     string
	Expected expectedHeuristic
}

// corpusDoc binds an answer-key entry to the document text it references.
// The DocPath is the value as written in the corpus file (used in log/report
// output); the actual on-disk path is resolved by loadCorpus relative to the
// corpus file's directory.
type corpusDoc struct {
	ID        string
	DocPath   string
	Text      string
	AnswerKey answerKey
}

// loadCorpus reads a JSON file containing an array of answer-key entries
// (one per document in the corpus). Each entry's "document" field is
// resolved relative to the corpus file's directory, so the file can sit
// alongside the documents it describes (e.g. test-data/batch1/prompt-corpus.json
// referencing tip1.txt … tip6.txt in the same directory).
//
// path may also be a directory, in which case loadCorpus looks for a file
// named "prompt-corpus.json" inside it. This keeps the existing default
// flag value usable when a user passes a batch directory.
func loadCorpus(path string) ([]corpusDoc, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat corpus path: %w", err)
	}
	if info.IsDir() {
		path = filepath.Join(path, "prompt-corpus.json")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read corpus file: %w", err)
	}
	var keys []answerKey
	if err := json.Unmarshal(raw, &keys); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}

	dir := filepath.Dir(path)
	docs := make([]corpusDoc, 0, len(keys))
	for i, key := range keys {
		if key.Document == "" {
			return nil, fmt.Errorf("%s entry %d: missing required \"document\" field", path, i)
		}
		docPath := key.Document
		if !filepath.IsAbs(docPath) {
			docPath = filepath.Join(dir, docPath)
		}
		text, err := os.ReadFile(docPath)
		if err != nil {
			return nil, fmt.Errorf("read document %s referenced by %s: %w", docPath, path, err)
		}
		id := strings.TrimSuffix(filepath.Base(key.Document), filepath.Ext(key.Document))
		docs = append(docs, corpusDoc{
			ID:        id,
			DocPath:   key.Document,
			Text:      string(text),
			AnswerKey: key,
		})
	}
	return docs, nil
}

// loadProductionPrompts reads the production document-heuristics prompt files
// from disk so the corpus tool exercises the same prompt the HTTP server
// actually uses. Pointing --prompts at a different directory lets a user
// experiment with edits without rebuilding the binary.
func loadProductionPrompts(dir string) (system, userTpl string, err error) {
	sysBytes, err := os.ReadFile(filepath.Join(dir, "document_heuristics_system.txt"))
	if err != nil {
		return "", "", fmt.Errorf("read system prompt: %w", err)
	}
	userBytes, err := os.ReadFile(filepath.Join(dir, "document_heuristics_user.txt"))
	if err != nil {
		return "", "", fmt.Errorf("read user prompt: %w", err)
	}
	return string(sysBytes), string(userBytes), nil
}

// renderUserPrompt substitutes the document text into the production user
// template. Mirrors backend/prompts.go without depending on it.
func renderUserPrompt(template, documentText string) string {
	return strings.Replace(template, "{{DOCUMENT_TEXT}}", documentText, 1)
}
