package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestMapleClientChatCompletion(t *testing.T) {
	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.String() != "http://maple.test/v1/chat/completions" {
				t.Errorf("url = %q, want http://maple.test/v1/chat/completions", r.URL.String())
			}
			if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
				t.Errorf("Authorization = %q, want Bearer test-key", got)
			}

			var req chatRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if req.Model != "test-model" {
				t.Errorf("model = %q, want test-model", req.Model)
			}
			if len(req.Messages) != 1 || req.Messages[0].Content != "summarize this" {
				t.Errorf("messages = %#v", req.Messages)
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Body:       io.NopCloser(strings.NewReader(buildChatResponse("done"))),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Request:    r,
			}, nil
		}),
	}

	client := mapleClient{
		baseURL:    "http://maple.test",
		apiKey:     "test-key",
		httpClient: httpClient,
	}
	got, err := client.ChatCompletion(context.Background(), chatRequest{
		Model: "test-model",
		Messages: []chatMessage{
			{Role: "user", Content: "summarize this"},
		},
	})
	if err != nil {
		t.Fatalf("ChatCompletion returned error: %v", err)
	}
	if got != "done" {
		t.Errorf("content = %q, want done", got)
	}
}

func TestMapleClientChatCompletionRequiresAPIKey(t *testing.T) {
	client := mapleClient{
		baseURL: "http://maple.test",
		httpClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				t.Fatal("transport should not be called without an API key")
				return nil, nil
			}),
		},
	}

	_, err := client.ChatCompletion(context.Background(), chatRequest{
		Model: "test-model",
		Messages: []chatMessage{
			{Role: "user", Content: "summarize this"},
		},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "MAPLE_API_KEY is required" {
		t.Errorf("error = %q, want MAPLE_API_KEY is required", err.Error())
	}
}

func TestMapleClientChatCompletionHandlesMapleErrors(t *testing.T) {
	client := mapleClient{
		baseURL: "http://maple.test",
		apiKey:  "test-key",
		httpClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Body: io.NopCloser(strings.NewReader(`{
						"error": { "type": "bad_request", "message": "invalid prompt" }
					}`)),
					Request: r,
				}, nil
			}),
		},
	}

	_, err := client.ChatCompletion(context.Background(), chatRequest{
		Model: "test-model",
		Messages: []chatMessage{
			{Role: "user", Content: "summarize this"},
		},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "maple error bad_request: invalid prompt" {
		t.Errorf("error = %q, want maple error bad_request: invalid prompt", err.Error())
	}
}

func buildChatResponse(content string) string {
	respBody, err := json.Marshal(chatResponse{
		Choices: []struct {
			Message chatMessage `json:"message"`
		}{
			{Message: chatMessage{Role: "assistant", Content: content}},
		},
	})
	if err != nil {
		panic(err)
	}
	return string(respBody)
}

type recordingChatCompleter struct {
	requests []chatRequest
	contents []string
}

func (c *recordingChatCompleter) ChatCompletion(_ context.Context, req chatRequest) (string, error) {
	c.requests = append(c.requests, req)
	content := c.contents[0]
	c.contents = c.contents[1:]
	return content, nil
}

func TestMapleClassifierUsesInjectedChatCompleter(t *testing.T) {
	completer := &recordingChatCompleter{
		contents: []string{`{"documents":[{"id":"doc-1","topic":{"title":"Ops","category":"ops","confidence":0.9},"document_type":{"category":"memo","confidence":0.8},"sensitivity":{"level":2,"label":"medium","confidence":0.7},"rationale":"ops memo","claims":[]}]}`},
	}
	classifier := mapleClassifier{
		model:  "classification-model",
		client: completer,
	}

	report, err := classifier.Classify(context.Background(), []classifiedInput{
		{ID: "doc-1", Filename: "memo.txt", Content: "memo body"},
	}, []categoryCandidate{{ID: 7, Title: "Existing", Description: "Existing category"}})
	if err != nil {
		t.Fatalf("Classify returned error: %v", err)
	}
	if len(report.Documents) != 1 || report.Documents[0].ID != "doc-1" {
		t.Fatalf("documents = %#v", report.Documents)
	}
	if len(completer.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(completer.requests))
	}
	if completer.requests[0].Model != "classification-model" {
		t.Errorf("model = %q, want classification-model", completer.requests[0].Model)
	}
	if completer.requests[0].ResponseFormat.Type != "json_object" {
		t.Errorf("response format = %q, want json_object", completer.requests[0].ResponseFormat.Type)
	}
}

func TestMapleClassifierClassifyDocumentReturnsParsedHeuristics(t *testing.T) {
	completer := &recordingChatCompleter{
		contents: []string{`{"heuristics":[
			{"name":"consistency","score":"high","explanation":"coherent timeline"},
			{"name":"references","score":"medium","explanation":"some dates and a PO"},
			{"name":"emotive_language","score":"low","explanation":"factual tone"},
			{"name":"ideology_or_incentives","score":"low","explanation":"no agenda"}
		]}`},
	}
	classifier := mapleClassifier{model: "doc-model", client: completer}

	hs, err := classifier.ClassifyDocument(context.Background(), classifiedInput{
		ID:      "memo.txt",
		Content: "MEMO BODY HERE",
	})
	if err != nil {
		t.Fatalf("ClassifyDocument returned error: %v", err)
	}
	if len(hs) != 4 {
		t.Fatalf("heuristics = %d, want 4", len(hs))
	}

	if len(completer.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(completer.requests))
	}
	req := completer.requests[0]
	if req.Model != "doc-model" {
		t.Errorf("model = %q, want doc-model", req.Model)
	}
	if req.Temperature != 0 {
		t.Errorf("temperature = %v, want 0", req.Temperature)
	}
	if req.ResponseFormat.Type != "json_object" {
		t.Errorf("response_format = %q, want json_object", req.ResponseFormat.Type)
	}
	if len(req.Messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(req.Messages))
	}
	prompt := req.Messages[0].Content
	if strings.Contains(prompt, documentTextPlaceholder) {
		t.Error("rendered prompt still contains the placeholder")
	}
	if !strings.Contains(prompt, "MEMO BODY HERE") {
		t.Error("rendered prompt missing the document body")
	}

	// score → Rating, explanation → Description
	wantRating := map[string]string{
		"consistency":            "high",
		"references":             "medium",
		"emotive_language":       "low",
		"ideology_or_incentives": "low",
	}
	for _, h := range hs {
		if h.Rating != wantRating[h.Name] {
			t.Errorf("%s rating = %q, want %q", h.Name, h.Rating, wantRating[h.Name])
		}
		if h.Description == "" {
			t.Errorf("%s description (mapped from explanation) is empty", h.Name)
		}
	}
}

func TestMapleClassifierClassifyDocumentRepairsInvalidJSON(t *testing.T) {
	completer := &recordingChatCompleter{
		contents: []string{
			`not json at all`,
			`{"heuristics":[{"name":"consistency","score":"high","explanation":"ok"}]}`,
		},
	}
	classifier := mapleClassifier{model: "doc-model", client: completer}

	hs, err := classifier.ClassifyDocument(context.Background(), classifiedInput{Content: "x"})
	if err != nil {
		t.Fatalf("ClassifyDocument returned error after repair: %v", err)
	}
	if len(hs) != 1 || hs[0].Name != "consistency" {
		t.Errorf("heuristics = %#v", hs)
	}
	if len(completer.requests) != 2 {
		t.Errorf("requests = %d, want 2 (initial + repair)", len(completer.requests))
	}
}

func TestParseDocumentHeuristicsMapsFields(t *testing.T) {
	hs, err := parseDocumentHeuristics(`{"heuristics":[
		{"name":"consistency","score":"high","explanation":"coherent"},
		{"name":"references","score":"low","explanation":"vague"}
	]}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(hs) != 2 {
		t.Fatalf("heuristics = %d, want 2", len(hs))
	}
	if hs[0].Name != "consistency" || hs[0].Rating != "high" || hs[0].Description != "coherent" {
		t.Errorf("hs[0] = %#v", hs[0])
	}
	if hs[1].Name != "references" || hs[1].Rating != "low" || hs[1].Description != "vague" {
		t.Errorf("hs[1] = %#v", hs[1])
	}
}

func TestParseDocumentHeuristicsRejectsBadJSON(t *testing.T) {
	if _, err := parseDocumentHeuristics(`{not json`); err == nil {
		t.Error("expected error on invalid JSON")
	}
}

func TestParseDocumentHeuristicsRejectsEmptyHeuristics(t *testing.T) {
	if _, err := parseDocumentHeuristics(`{"heuristics":[]}`); err == nil {
		t.Error("expected error on empty heuristics array")
	}
}

func TestRenderDocumentHeuristicsPromptSubstitutes(t *testing.T) {
	out := renderDocumentHeuristicsPrompt("HELLO WORLD")
	if strings.Contains(out, documentTextPlaceholder) {
		t.Errorf("placeholder still present after render: %s", documentTextPlaceholder)
	}
	if !strings.Contains(out, "HELLO WORLD") {
		t.Error("rendered prompt missing the substituted document text")
	}
	for _, h := range []string{"consistency", "references", "emotive_language", "ideology_or_incentives"} {
		if !strings.Contains(out, h) {
			t.Errorf("rendered prompt missing heuristic name %q", h)
		}
	}
}

func TestParseClassificationContentAcceptsStringClaims(t *testing.T) {
	report, err := parseClassificationContent(`{
		"documents": [
			{
				"id": "memo.txt",
				"topic": {
					"id": 0,
					"title": "Irregular Payment to Atlas Holdings",
					"description": "Payment without purchase order."
				},
				"claims": [
					"Atlas Holdings was paid without a purchase order.",
					"The memo instructs not to share this information with audit."
				]
			}
		]
	}`)
	if err != nil {
		t.Fatalf("parseClassificationContent returned error: %v", err)
	}
	if got := len(report.Documents[0].Claims); got != 2 {
		t.Fatalf("claims = %d, want 2", got)
	}
	if got := report.Documents[0].Claims[0].Claim; got != "Atlas Holdings was paid without a purchase order." {
		t.Errorf("claim = %q", got)
	}
}

func TestParseClassificationContentExtractsWrappedJSONObject(t *testing.T) {
	report, err := parseClassificationContent("Here is the JSON:\n" + validClassificationContent() + "\nextra trailing text")
	if err != nil {
		t.Fatalf("parseClassificationContent returned error: %v", err)
	}
	if got := report.Documents[0].ID; got != "doc-1" {
		t.Errorf("document id = %q, want doc-1", got)
	}
}

func TestParseClassificationContentDetectsMapleQuotaMessage(t *testing.T) {
	_, err := parseClassificationContent(`{"documents":[{"id":"memo.txt","topic":{"id":电量不足，请充值后使用。`)
	if !errors.Is(err, errMapleQuotaExhausted) {
		t.Fatalf("error = %v, want maple quota exhausted", err)
	}
}

func TestParseClassificationContentRepairsInvalidNumericIDToken(t *testing.T) {
	report, err := parseClassificationContent(`{
		"documents": [
			{
				"id": "memo.txt",
				"topic": {
					"id": 充电,
					"title": "Atlas Holdings Payment Irregularity",
					"description": "Payments made without purchase order documentation."
				},
				"document_type": {"category": "memo", "confidence": 0.8},
				"sensitivity": {"level": 3, "label": "high", "confidence": 0.9},
				"rationale": "Contains evidence of financial irregularity.",
				"claims": []
			}
		]
	}`)
	if err != nil {
		t.Fatalf("parseClassificationContent returned error: %v", err)
	}
	if got := report.Documents[0].Topic.ID; got != 0 {
		t.Errorf("topic id = %d, want 0", got)
	}
}

func TestMapleClassifierDoesNotRepairMapleQuotaMessage(t *testing.T) {
	completer := &recordingChatCompleter{
		contents: []string{`{"documents":[{"id":"memo.txt","topic":{"id":电量不足，请充值后使用。`},
	}
	classifier := mapleClassifier{
		model:  "classification-model",
		client: completer,
	}

	_, err := classifier.Classify(context.Background(), []classifiedInput{
		{ID: "memo.txt", Filename: "memo.txt", Content: "memo body"},
	}, nil)
	if !errors.Is(err, errMapleQuotaExhausted) {
		t.Fatalf("error = %v, want maple quota exhausted", err)
	}
	if len(completer.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(completer.requests))
	}
}

func TestBuildRepairRequestIncludesClassificationSchema(t *testing.T) {
	req := buildRepairRequest("test-model", "{bad json")
	if len(req.Messages) == 0 {
		t.Fatal("repair request has no messages")
	}
	if !strings.Contains(req.Messages[0].Content, "Each claim must be an object") {
		t.Errorf("repair prompt does not include claim schema: %q", req.Messages[0].Content)
	}
}

func validClassificationContent() string {
	return `{"documents":[{"id":"doc-1","topic":{"title":"Ops","category":"ops","confidence":0.9},"document_type":{"category":"memo","confidence":0.8},"sensitivity":{"level":2,"label":"medium","confidence":0.7},"rationale":"ops memo","claims":[]}]}`
}
