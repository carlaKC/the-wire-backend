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
		contents: []string{`{"documents":[{"id":"doc-1","topic":{"title":"Ops","topic":"ops","confidence":0.9,"importance":{"score":"medium","explanation":"Operational concern."}},"document_type":{"topic":"memo","confidence":0.8},"sensitivity":{"level":2,"label":"medium","confidence":0.7},"rationale":"ops memo","claims":[]}]}`},
	}
	classifier := mapleClassifier{
		model:  "classification-model",
		client: completer,
	}

	report, err := classifier.Classify(context.Background(), []classifiedInput{
		{ID: "doc-1", Filename: "memo.txt", Content: "memo body"},
	}, []topicCandidate{{ID: 7, Title: "Existing", Description: "Existing topic"}})
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
	if report.Documents[0].Topic.Importance.Score != "medium" {
		t.Errorf("importance score = %q, want medium", report.Documents[0].Topic.Importance.Score)
	}
	if !strings.Contains(completer.requests[0].Messages[0].Content, "topic.importance") {
		t.Errorf("classification prompt does not request topic.importance: %q", completer.requests[0].Messages[0].Content)
	}
}

func TestMapleClassifierClassifyDocumentReturnsParsedHeuristics(t *testing.T) {
	completer := &recordingChatCompleter{
		contents: []string{`{"heuristics":[
			{"name":"consistency","signal":"positive","score":"high","explanation":"coherent timeline"},
			{"name":"references","signal":"positive","score":"medium","explanation":"some dates and a PO"},
			{"name":"emotive_language","signal":"negative","score":"low","explanation":"factual tone"},
			{"name":"ideology","signal":"negative","score":"low","explanation":"no agenda"}
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
	if len(req.Messages) != 2 {
		t.Fatalf("messages = %d, want 2 (system + user)", len(req.Messages))
	}
	if req.Messages[0].Role != "system" || req.Messages[1].Role != "user" {
		t.Errorf("message roles = %q,%q; want system,user", req.Messages[0].Role, req.Messages[1].Role)
	}
	user := req.Messages[1].Content
	if strings.Contains(user, documentTextPlaceholder) {
		t.Error("rendered user prompt still contains the placeholder")
	}
	if !strings.Contains(user, "MEMO BODY HERE") {
		t.Error("rendered user prompt missing the document body")
	}

	// score → Rating, explanation → Description, signal passes through verbatim
	wantRating := map[string]string{
		"consistency":            "high",
		"references":             "medium",
		"emotive_language":       "low",
		"ideology": "low",
	}
	wantSignal := map[string]string{
		"consistency":            "positive",
		"references":             "positive",
		"emotive_language":       "negative",
		"ideology": "negative",
	}
	for _, h := range hs {
		if h.Rating != wantRating[h.Name] {
			t.Errorf("%s rating = %q, want %q", h.Name, h.Rating, wantRating[h.Name])
		}
		if h.Signal != wantSignal[h.Name] {
			t.Errorf("%s signal = %q, want %q", h.Name, h.Signal, wantSignal[h.Name])
		}
		if h.Description == "" {
			t.Errorf("%s description (mapped from explanation) is empty", h.Name)
		}
	}
}

func TestMapleClassifierClassifyDocumentRepairsInvalidJSON(t *testing.T) {
	// Mimics the real production bug: first response is mostly-valid JSON
	// missing one quote on an explanation field. parseDocumentHeuristics
	// rejects it, ClassifyDocument issues a schema-aware repair, the model
	// returns valid heuristics on the second try.
	brokenFirstResponse := `{
		"heuristics": [
			{"name":"consistency","signal":"positive","score":"medium","explanation":"ok"},
			{"name":"references","signal":"positive","score":"low","explanation": No verifiable references."}
		]
	}`
	completer := &recordingChatCompleter{
		contents: []string{
			brokenFirstResponse,
			`{"heuristics":[
				{"name":"consistency","signal":"positive","score":"medium","explanation":"ok"},
				{"name":"references","signal":"positive","score":"low","explanation":"no verifiable references"},
				{"name":"emotive_language","signal":"negative","score":"low","explanation":"calm"},
				{"name":"ideology","signal":"negative","score":"low","explanation":"none"}
			]}`,
		},
	}
	classifier := mapleClassifier{model: "doc-model", client: completer}

	hs, err := classifier.ClassifyDocument(context.Background(), classifiedInput{Content: "x"})
	if err != nil {
		t.Fatalf("ClassifyDocument returned error after repair: %v", err)
	}
	if len(hs) != 4 {
		t.Errorf("heuristics = %d, want 4", len(hs))
	}
	if len(completer.requests) != 2 {
		t.Fatalf("requests = %d, want 2 (initial + repair)", len(completer.requests))
	}

	// The repair must be schema-aware (pin the heuristics schema), not the
	// generic "fix this JSON" prompt — without the schema anchor the model
	// drifts to other shapes (observed in production: drifted to the
	// classification report's documents/claims schema).
	repair := completer.requests[1]
	if len(repair.Messages) < 1 {
		t.Fatal("repair request had no messages")
	}
	system := repair.Messages[0].Content
	for _, want := range []string{"consistency", "references", "emotive_language", "ideology", `"signal"`, `"score"`, `"explanation"`} {
		if !strings.Contains(system, want) {
			t.Errorf("repair system prompt missing %q (schema must be pinned). got: %s", want, system)
		}
	}
}

func TestMapleClassifierClassifyGroupReturnsParsedHeuristics(t *testing.T) {
	completer := &recordingChatCompleter{
		contents: []string{`{"heuristics":[
			{"name":"corroboration","signal":"positive","score":"high","explanation":"docs corroborate"},
			{"name":"shared_references","signal":"positive","score":"medium","explanation":"some shared invoice ids"},
			{"name":"coordinated_framing","signal":"negative","score":"low","explanation":"varied tone"},
			{"name":"shared_agenda","signal":"negative","score":"low","explanation":"no shared agenda"},
			{"name":"contested_narrative","signal":"positive","score":"high","explanation":"one document rebuts another"},
			{"name":"timeline_coherence","signal":"positive","score":"medium","explanation":"dates mostly align"},
			{"name":"temporal_scope","signal":"positive","score":"high","explanation":"pattern spans multiple quarters"}
		]}`},
	}
	classifier := mapleClassifier{model: "group-model", client: completer}

	hs, err := classifier.ClassifyGroup(context.Background(), []classifiedInput{
		{ID: "a.txt", Filename: "a.txt", Content: "alpha body"},
		{ID: "b.txt", Filename: "b.txt", Content: "beta body"},
	}, "Procurement")
	if err != nil {
		t.Fatalf("ClassifyGroup returned error: %v", err)
	}
	if len(hs) != 7 {
		t.Fatalf("heuristics = %d, want 7", len(hs))
	}

	if len(completer.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(completer.requests))
	}
	req := completer.requests[0]
	if req.Model != "group-model" {
		t.Errorf("model = %q, want group-model", req.Model)
	}
	if req.ResponseFormat.Type != "json_object" {
		t.Errorf("response_format = %q, want json_object", req.ResponseFormat.Type)
	}
	prompt := req.Messages[0].Content
	if strings.Contains(prompt, documentsTextPlaceholder) {
		t.Error("rendered prompt still contains documents placeholder")
	}
	if strings.Contains(prompt, topicTitlePlaceholder) {
		t.Error("rendered prompt still contains topic title placeholder")
	}
	for _, want := range []string{"Procurement", "--- Document: a.txt ---", "alpha body", "--- Document: b.txt ---", "beta body"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("rendered prompt missing %q", want)
		}
	}
	for _, want := range []string{"contested_narrative", "review-salience signal", "not a reason to filter documents"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("rendered prompt missing contested narrative guidance %q", want)
		}
	}
	for _, want := range []string{"timeline_coherence", "temporal_scope", "sustained or recurring conduct", "single incident"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("rendered prompt missing timeline guidance %q", want)
		}
	}

	wantRating := map[string]string{
		"corroboration":       "high",
		"shared_references":   "medium",
		"coordinated_framing": "low",
		"shared_agenda":       "low",
		"contested_narrative": "high",
		"timeline_coherence":  "medium",
		"temporal_scope":      "high",
	}
	for _, h := range hs {
		if h.Rating != wantRating[h.Name] {
			t.Errorf("%s rating = %q, want %q", h.Name, h.Rating, wantRating[h.Name])
		}
		if h.Signal == "" {
			t.Errorf("%s signal is empty", h.Name)
		}
	}
}

func TestMapleClassifierClassifyGroupRepairsInvalidJSON(t *testing.T) {
	completer := &recordingChatCompleter{
		contents: []string{
			`{"heuristics":[{"name":"corroboration","signal":"positive","score":"high","explanation": missing quotes}]}`,
			`{"heuristics":[{"name":"corroboration","signal":"positive","score":"high","explanation":"corroborate"}]}`,
		},
	}
	classifier := mapleClassifier{model: "group-model", client: completer}

	hs, err := classifier.ClassifyGroup(context.Background(), []classifiedInput{
		{ID: "x.txt", Content: "x"},
	}, "Procurement")
	if err != nil {
		t.Fatalf("ClassifyGroup returned error after repair: %v", err)
	}
	if len(hs) != 1 {
		t.Errorf("heuristics = %d, want 1", len(hs))
	}
	if len(completer.requests) != 2 {
		t.Fatalf("requests = %d, want 2 (initial + repair)", len(completer.requests))
	}
	system := completer.requests[1].Messages[0].Content
	for _, want := range []string{`"signal"`, `"score"`, `"explanation"`} {
		if !strings.Contains(system, want) {
			t.Errorf("repair system prompt missing %q. got: %s", want, system)
		}
	}
}

func TestBundleDocumentsForGroupScan(t *testing.T) {
	out := bundleDocumentsForGroupScan([]classifiedInput{
		{ID: "a.txt", Filename: "a.txt", Content: "alpha\n"},
		{ID: "b.txt", Content: "beta"},
	})
	if !strings.Contains(out, "--- Document: a.txt ---") {
		t.Error("missing first document header")
	}
	if !strings.Contains(out, "--- Document: b.txt ---") {
		t.Error("missing second document header")
	}
	if !strings.Contains(out, "alpha") || !strings.Contains(out, "beta") {
		t.Error("missing document content")
	}
}

func TestParseDocumentHeuristicsMapsFields(t *testing.T) {
	hs, err := parseDocumentHeuristics(`{"heuristics":[
		{"name":"consistency","signal":"positive","score":"high","explanation":"coherent"},
		{"name":"references","signal":"positive","score":"low","explanation":"vague"}
	]}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(hs) != 2 {
		t.Fatalf("heuristics = %d, want 2", len(hs))
	}
	if hs[0].Name != "consistency" || hs[0].Signal != "positive" || hs[0].Rating != "high" || hs[0].Description != "coherent" {
		t.Errorf("hs[0] = %#v", hs[0])
	}
	if hs[1].Name != "references" || hs[1].Signal != "positive" || hs[1].Rating != "low" || hs[1].Description != "vague" {
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

func TestParseDocumentHeuristicsTolerantOfMarkdownFences(t *testing.T) {
	hs, err := parseDocumentHeuristics("```json\n" + `{"heuristics":[{"name":"consistency","signal":"positive","score":"high","explanation":"coherent"}]}` + "\n```")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(hs) != 1 || hs[0].Name != "consistency" {
		t.Fatalf("hs = %#v", hs)
	}
}

func TestRenderDocumentHeuristicsUserSubstitutes(t *testing.T) {
	user := renderDocumentHeuristicsUser("HELLO WORLD")
	if strings.Contains(user, documentTextPlaceholder) {
		t.Errorf("placeholder still present after render: %s", documentTextPlaceholder)
	}
	if !strings.Contains(user, "HELLO WORLD") {
		t.Error("rendered user prompt missing the substituted document text")
	}
	for _, h := range []string{"consistency", "references", "emotive_language", "ideology"} {
		if !strings.Contains(documentHeuristicsSystemPrompt, h) {
			t.Errorf("system prompt missing heuristic name %q", h)
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

func TestParseClassificationContentAcceptsLooseClaimFields(t *testing.T) {
	report, err := parseClassificationContent(`{
		"documents": [
			{
				"id": "memo.txt",
				"topic": {
					"id": 0,
					"title": "Irregular Payment to Atlas Holdings",
					"description": "Payment without purchase order."
				},
				"document_type": {"topic": "memo", "confidence": 0.8},
				"sensitivity": {"level": 3, "label": "high", "confidence": 0.9},
				"rationale": "Contains evidence of financial irregularity.",
				"claims": [
					{
						"id": 7,
						"claim": "Atlas Holdings was paid without a purchase order.",
						"topic": "payment irregularity",
						"confidence": 0.9,
						"evidence": "paid without a purchase order",
						"validation": "supported",
						"sensitivity": {"level": 3, "label": "high", "confidence": 0.9},
						"flags": []
					}
				]
			}
		]
	}`)
	if err != nil {
		t.Fatalf("parseClassificationContent returned error: %v", err)
	}
	claim := report.Documents[0].Claims[0]
	if claim.ID != "7" {
		t.Errorf("claim id = %q, want 7", claim.ID)
	}
	if claim.Validation.Status != "supported" {
		t.Errorf("validation status = %q, want supported", claim.Validation.Status)
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

// When the model response is cut off mid-stream (typically because an upstream
// max-token cap chopped it), the decoder returns io.ErrUnexpectedEOF. The
// error must surface that as truncation + a tail excerpt — without this hint,
// debugging requires guessing whether the failure was syntax or truncation.
func TestParseClassificationContentDiagnosticsForTruncatedJSON(t *testing.T) {
	truncated := `{"documents":[{"id":"a","topic":{"id":1,"title":"T",`
	_, err := parseClassificationContent(truncated)
	if err == nil {
		t.Fatal("expected error on truncated JSON")
	}
	msg := err.Error()
	if !strings.Contains(msg, "truncated") {
		t.Errorf("error should hint truncation, got: %v", err)
	}
	if !strings.Contains(msg, "tail:") {
		t.Errorf("error should include tail excerpt, got: %v", err)
	}
}

// When the model response has a syntax error somewhere past the head excerpt,
// json.Decoder returns *json.SyntaxError with a byte Offset. The error must
// surface that offset + a window around it — the head excerpt alone hides the
// failure point on multi-kilobyte responses.
func TestParseClassificationContentDiagnosticsForSyntaxError(t *testing.T) {
	bad := `{"documents":[{"id":"a","topic":{"id":1,"title":"T"}},,]}`
	_, err := parseClassificationContent(bad)
	if err == nil {
		t.Fatal("expected error on bad JSON syntax")
	}
	msg := err.Error()
	if !strings.Contains(msg, "offset") {
		t.Errorf("error should include byte offset, got: %v", err)
	}
	if !strings.Contains(msg, "<<HERE>>") {
		t.Errorf("error should include a window marker, got: %v", err)
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
				"document_type": {"topic": "memo", "confidence": 0.8},
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

func TestBuildCaseClassificationRepairRequestIncludesSchema(t *testing.T) {
	req := buildCaseClassificationRepairRequest("test-model", "{bad json")
	if len(req.Messages) == 0 {
		t.Fatal("repair request has no messages")
	}
	if !strings.Contains(req.Messages[0].Content, "Each claim must be an object") {
		t.Errorf("repair prompt does not include claim schema: %q", req.Messages[0].Content)
	}
}

func validClassificationContent() string {
	return `{"documents":[{"id":"doc-1","topic":{"title":"Ops","topic":"ops","confidence":0.9},"document_type":{"topic":"memo","confidence":0.8},"sensitivity":{"level":2,"label":"medium","confidence":0.7},"rationale":"ops memo","claims":[]}]}`
}
