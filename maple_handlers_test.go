package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

type classifierCall struct {
	documents      []classifiedInput
	existingTopics []topicCandidate
}

type controlledClassifier struct {
	report           classificationReport
	err              error
	release          chan struct{}
	calls            chan classifierCall
	documentReport   documentClassification
	documentErr      error
	documentReleases chan struct{}
	documentCalls    chan classifiedInput
	groupReport      []heuristic
	groupErr         error
	groupCalls       chan groupCall
}

type groupCall struct {
	documents  []classifiedInput
	topicTitle string
}

func newControlledClassifier(report classificationReport, err error) *controlledClassifier {
	return &controlledClassifier{
		report: report,
		err:    err,
		calls:  make(chan classifierCall, 1),
	}
}

func (c *controlledClassifier) Classify(_ context.Context, documents []classifiedInput, existingTopics []topicCandidate) (classificationReport, error) {
	c.calls <- classifierCall{
		documents:      append([]classifiedInput{}, documents...),
		existingTopics: append([]topicCandidate{}, existingTopics...),
	}
	if c.release != nil {
		<-c.release
	}
	return c.report, c.err
}

func (c *controlledClassifier) ClassifyDocument(_ context.Context, document classifiedInput) (documentClassification, error) {
	if c.documentCalls != nil {
		c.documentCalls <- document
	}
	if c.documentReleases != nil {
		<-c.documentReleases
	}
	return c.documentReport, c.documentErr
}

func (c *controlledClassifier) ClassifyGroup(_ context.Context, documents []classifiedInput, topicTitle string) ([]heuristic, error) {
	if c.groupCalls != nil {
		c.groupCalls <- groupCall{
			documents:  append([]classifiedInput{}, documents...),
			topicTitle: topicTitle,
		}
	}
	return c.groupReport, c.groupErr
}

func TestMapleCreateCaseStartsProcessingAndCompletes(t *testing.T) {
	classifier := newControlledClassifier(classificationReport{Documents: []classifiedDocument{
		classifiedDoc("memo.txt", classifiedTopic{
			Title:       "Procurement",
			Description: "Vendor approval and purchasing issues.",
			Topic:       "procurement",
			Confidence:  0.95,
		}),
	}}, nil)
	classifier.release = make(chan struct{})
	srv := newMapleServer(classifier)

	caseID := postMapleCase(t, srv, []docInput{
		{Filename: "memo.txt", Content: "Vendor Atlas lacked a purchase order."},
	})
	call := <-classifier.calls
	if got := len(call.documents); got != 1 {
		t.Fatalf("classifier document count = %d, want 1", got)
	}
	if call.documents[0].ID != "memo.txt" {
		t.Fatalf("classifier document id = %q, want memo.txt", call.documents[0].ID)
	}

	processing := getMapleCase(t, srv, caseID)
	if processing.Status != statusProcessing {
		t.Fatalf("initial status = %q, want %q", processing.Status, statusProcessing)
	}
	if got := len(processing.Topics); got != 0 {
		t.Fatalf("initial topic count = %d, want 0", got)
	}

	close(classifier.release)
	complete := waitMapleStatus(t, srv, caseID, statusComplete)
	if got := len(complete.Topics); got != 1 {
		t.Fatalf("complete topic count = %d, want 1", got)
	}
	topicID := complete.Topics[0].ID

	detail := getMapleTopic(t, srv, caseID, topicID)
	if got := len(detail.Topic.Heuristics); got == 0 {
		t.Fatal("topic heuristics were empty")
	}
	if detail.Topic.Triage != complete.Topics[0].Triage {
		t.Fatalf("detail triage = %q, summary triage = %q", detail.Topic.Triage, complete.Topics[0].Triage)
	}
	if detail.Topic.Description != complete.Topics[0].Description {
		t.Fatalf("detail description = %q, summary description = %q", detail.Topic.Description, complete.Topics[0].Description)
	}
	if !strings.Contains(detail.Topic.Description, "Triage: "+detail.Topic.Triage) {
		t.Fatalf("topic description does not include triage note: %q", detail.Topic.Description)
	}
	if !containsTopicHeuristicNamed(detail.Topic.Heuristics, "impact") {
		t.Errorf("topic heuristics missing impact. got: %v", topicHeuristicNames(detail.Topic.Heuristics))
	}
	docs := getMapleTopicDocuments(t, srv, caseID, topicID)
	if got := len(docs.Documents); got != 1 {
		t.Fatalf("topic document count = %d, want 1", got)
	}
}

func TestProcessCaseAttachesPerDocumentHeuristics(t *testing.T) {
	classifier := newControlledClassifier(classificationReport{Documents: []classifiedDocument{
		classifiedDoc("memo.txt", classifiedTopic{
			Title:       "Procurement",
			Description: "Vendor approval and purchasing issues.",
			Topic:       "procurement",
			Confidence:  0.95,
		}),
	}}, nil)
	classifier.documentReport = documentClassification{
		Heuristics: []heuristic{
			{Name: "consistency", Rating: "high", Description: "coherent timeline"},
			{Name: "references", Rating: "medium", Description: "PO referenced"},
			{Name: "emotive_language", Rating: "low", Description: "factual tone"},
			{Name: "ideology", Rating: "low", Description: "no agenda"},
		},
		FactsToVerify: []string{
			"Vendor Atlas is named in the memo without a purchase order on file.",
		},
	}
	srv := newMapleServer(classifier)

	caseID := postMapleCase(t, srv, []docInput{{Filename: "memo.txt", Content: "Vendor Atlas lacked a purchase order."}})
	complete := waitMapleStatus(t, srv, caseID, statusComplete)
	if got := len(complete.Topics); got != 1 {
		t.Fatalf("topic count = %d, want 1", got)
	}
	topicID := complete.Topics[0].ID

	docs := getMapleTopicDocuments(t, srv, caseID, topicID)
	if got := len(docs.Documents); got != 1 {
		t.Fatalf("document count = %d, want 1", got)
	}
	got := docs.Documents[0].Heuristics
	wantPromptNames := []string{"consistency", "references", "emotive_language", "ideology"}
	for _, name := range wantPromptNames {
		if !containsHeuristicNamed(got, name) {
			t.Errorf("document heuristics missing %q (from ClassifyDocument). got: %v", name, heuristicNames(got))
		}
	}
}

func TestProcessCaseRunsClassifyDocumentWhileClassifyCaseIsBlocked(t *testing.T) {
	classifier := newControlledClassifier(classificationReport{Documents: []classifiedDocument{
		classifiedDoc("memo.txt", classifiedTopic{
			Title: "Procurement",
			Topic: "procurement",
		}),
	}}, nil)
	classifier.release = make(chan struct{})
	classifier.documentCalls = make(chan classifiedInput, 1)
	srv := newMapleServer(classifier)

	caseID := postMapleCase(t, srv, []docInput{{Filename: "memo.txt", Content: "memo body"}})
	<-classifier.calls

	select {
	case call := <-classifier.documentCalls:
		if call.ID != "memo.txt" {
			t.Fatalf("ClassifyDocument called with id %q, want memo.txt", call.ID)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("ClassifyDocument did not start while Classify was blocked")
	}

	close(classifier.release)
	waitMapleStatus(t, srv, caseID, statusComplete)
}

func TestProcessCaseStartsGroupScanOnlyAfterCaseAndDocumentStagesComplete(t *testing.T) {
	classifier := newControlledClassifier(classificationReport{Documents: []classifiedDocument{
		classifiedDoc("memo-1.txt", classifiedTopic{Title: "Procurement", Topic: "procurement"}),
		classifiedDoc("memo-2.txt", classifiedTopic{Title: "Procurement", Topic: "procurement"}),
	}}, nil)
	classifier.release = make(chan struct{})
	classifier.documentReleases = make(chan struct{})
	classifier.documentCalls = make(chan classifiedInput, 2)
	classifier.documentReport = documentClassification{
		Heuristics: []heuristic{
			{Name: "emotive_language", Signal: "negative", Rating: "low"},
			{Name: "ideology", Signal: "negative", Rating: "low"},
		},
	}
	classifier.groupReport = []heuristic{{Name: "corroboration", Signal: "positive", Rating: "medium"}}
	classifier.groupCalls = make(chan groupCall, 1)
	srv := newMapleServer(classifier)

	caseID := postMapleCase(t, srv, []docInput{
		{Filename: "memo-1.txt", Content: "first memo"},
		{Filename: "memo-2.txt", Content: "second memo"},
	})
	<-classifier.calls
	for i := 0; i < 2; i++ {
		select {
		case <-classifier.documentCalls:
		case <-time.After(250 * time.Millisecond):
			t.Fatal("ClassifyDocument did not start for both documents")
		}
	}
	assertNoGroupCall(t, classifier.groupCalls)

	close(classifier.release)
	assertNoGroupCall(t, classifier.groupCalls)

	classifier.documentReleases <- struct{}{}
	classifier.documentReleases <- struct{}{}
	select {
	case call := <-classifier.groupCalls:
		if got := len(call.documents); got != 2 {
			t.Fatalf("group call documents = %d, want 2", got)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("ClassifyGroup did not start after case and document stages completed")
	}
	waitMapleStatus(t, srv, caseID, statusComplete)
}

func TestGetTopicDocumentsReturnsOnlyDocumentsForThatTopic(t *testing.T) {
	classifier := newControlledClassifier(classificationReport{Documents: []classifiedDocument{
		classifiedDoc("memo.txt", classifiedTopic{
			Title: "Procurement",
			Topic: "procurement",
		}),
		classifiedDoc("invoice.txt", classifiedTopic{
			Title: "Finance",
			Topic: "finance",
		}),
	}}, nil)
	classifier.documentReport = documentClassification{
		Heuristics: []heuristic{
			{Name: "consistency", Signal: "positive", Rating: "high", Description: "coherent"},
			{Name: "references", Signal: "positive", Rating: "medium", Description: "PO referenced"},
			{Name: "emotive_language", Signal: "negative", Rating: "low", Description: "factual"},
			{Name: "ideology", Signal: "negative", Rating: "low", Description: "no agenda"},
		},
	}
	srv := newMapleServer(classifier)

	caseID := postMapleCase(t, srv, []docInput{
		{Filename: "memo.txt", Content: "memo body"},
		{Filename: "invoice.txt", Content: "invoice body"},
	})
	complete := waitMapleStatus(t, srv, caseID, statusComplete)
	if got := len(complete.Topics); got != 2 {
		t.Fatalf("topic count = %d, want 2", got)
	}

	// Pick the Procurement topic and verify the endpoint only returns that
	// topic's documents (memo.txt), not the Finance one (invoice.txt).
	var procurementID int
	for _, c := range complete.Topics {
		if c.Title == "Procurement" {
			procurementID = c.ID
		}
	}
	if procurementID == 0 {
		t.Fatal("could not find Procurement topic id")
	}

	resp := getMapleTopicDocuments(t, srv, caseID, procurementID)
	if resp.CaseID != caseID {
		t.Errorf("case_id = %d, want %d", resp.CaseID, caseID)
	}
	if resp.TopicID != procurementID {
		t.Errorf("topic_id = %d, want %d", resp.TopicID, procurementID)
	}
	if got := len(resp.Documents); got != 1 {
		t.Fatalf("documents = %d, want 1 (filtered to topic); got: %#v", got, resp.Documents)
	}
	if resp.Documents[0].Filename != "memo.txt" {
		t.Errorf("document filename = %q, want memo.txt", resp.Documents[0].Filename)
	}
	if !containsHeuristicNamed(resp.Documents[0].Heuristics, "consistency") {
		t.Errorf("document missing prompt heuristic %q", "consistency")
	}
}

func TestGetTopicDocumentsUnknownCaseReturns404(t *testing.T) {
	srv := newMapleServer(newControlledClassifier(classificationReport{}, nil))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cases/999/topics/1/documents", nil)
	req.SetPathValue("case_id", "999")
	req.SetPathValue("topic_id", "1")
	rec := httptest.NewRecorder()
	srv.getTopicDocumentsHandler(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", rec.Code, rec.Body.String())
	}
}

func TestGetTopicDocumentsUnknownTopicReturns404(t *testing.T) {
	classifier := newControlledClassifier(classificationReport{Documents: []classifiedDocument{
		classifiedDoc("memo.txt", classifiedTopic{Title: "Procurement", Topic: "procurement"}),
	}}, nil)
	srv := newMapleServer(classifier)

	caseID := postMapleCase(t, srv, []docInput{{Filename: "memo.txt", Content: "memo body"}})
	waitMapleStatus(t, srv, caseID, statusComplete)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cases/"+strconv.Itoa(caseID)+"/topics/9999/documents", nil)
	req.SetPathValue("case_id", strconv.Itoa(caseID))
	req.SetPathValue("topic_id", "9999")
	rec := httptest.NewRecorder()
	srv.getTopicDocumentsHandler(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", rec.Code, rec.Body.String())
	}
}

func TestProcessCaseFailsCaseWhenClassifyDocumentFails(t *testing.T) {
	classifier := newControlledClassifier(classificationReport{Documents: []classifiedDocument{
		classifiedDoc("memo.txt", classifiedTopic{
			Title: "Procurement",
			Topic: "procurement",
		}),
	}}, nil)
	classifier.documentErr = errors.New("simulated per-doc failure")
	srv := newMapleServer(classifier)

	caseID := postMapleCase(t, srv, []docInput{{Filename: "memo.txt", Content: "memo body"}})
	failed := waitMapleStatus(t, srv, caseID, statusFailed)
	if failed.Status != statusFailed {
		t.Fatalf("status = %q, want %q", failed.Status, statusFailed)
	}
}

func TestProcessCaseRunsGroupScanForUnfilteredDocuments(t *testing.T) {
	classifier := newControlledClassifier(classificationReport{Documents: []classifiedDocument{
		classifiedDoc("memo-1.txt", classifiedTopic{Title: "Procurement", Topic: "procurement"}),
		classifiedDoc("memo-2.txt", classifiedTopic{Title: "Procurement", Topic: "procurement"}),
	}}, nil)
	classifier.documentReport = documentClassification{
		Heuristics: []heuristic{
			{Name: "consistency", Signal: "positive", Rating: "high"},
			{Name: "references", Signal: "positive", Rating: "high"},
			{Name: "emotive_language", Signal: "negative", Rating: "low"},
			{Name: "ideology", Signal: "negative", Rating: "low"},
		},
	}
	classifier.groupReport = []heuristic{
		{Name: "corroboration", Signal: "positive", Rating: "high", Description: "docs corroborate"},
		{Name: "shared_references", Signal: "positive", Rating: "medium", Description: "shared invoice ids"},
		{Name: "contested_narrative", Signal: "positive", Rating: "high", Description: "one document rebuts another"},
		{Name: "timeline_coherence", Signal: "positive", Rating: "medium", Description: "dates mostly align"},
		{Name: "temporal_scope", Signal: "positive", Rating: "high", Description: "pattern spans multiple quarters"},
	}
	classifier.groupCalls = make(chan groupCall, 1)
	srv := newMapleServer(classifier)

	caseID := postMapleCase(t, srv, []docInput{
		{Filename: "memo-1.txt", Content: "first procurement memo"},
		{Filename: "memo-2.txt", Content: "second procurement memo"},
	})
	complete := waitMapleStatus(t, srv, caseID, statusComplete)

	select {
	case call := <-classifier.groupCalls:
		if got := len(call.documents); got != 2 {
			t.Fatalf("group call documents = %d, want 2", got)
		}
		if call.topicTitle != "Procurement" {
			t.Errorf("group call topic title = %q, want Procurement", call.topicTitle)
		}
	default:
		t.Fatal("expected ClassifyGroup to be called")
	}

	topicID := complete.Topics[0].ID
	detail := getMapleTopic(t, srv, caseID, topicID)
	if !containsTopicHeuristicNamed(detail.Topic.Heuristics, "corroboration") {
		t.Errorf("topic heuristics missing group heuristic. got: %v", topicHeuristicNames(detail.Topic.Heuristics))
	}
	if !containsTopicHeuristicNamed(detail.Topic.Heuristics, "contested_narrative") {
		t.Errorf("topic heuristics missing contested narrative heuristic. got: %v", topicHeuristicNames(detail.Topic.Heuristics))
	}
	if !containsTopicHeuristicNamed(detail.Topic.Heuristics, "timeline_coherence") {
		t.Errorf("topic heuristics missing timeline coherence heuristic. got: %v", topicHeuristicNames(detail.Topic.Heuristics))
	}
	if !containsTopicHeuristicNamed(detail.Topic.Heuristics, "temporal_scope") {
		t.Errorf("topic heuristics missing temporal scope heuristic. got: %v", topicHeuristicNames(detail.Topic.Heuristics))
	}
}

func TestProcessCaseExcludesFilteredDocumentsFromGroupScan(t *testing.T) {
	classifier := newControlledClassifier(classificationReport{Documents: []classifiedDocument{
		classifiedDoc("filtered.txt", classifiedTopic{Title: "Procurement", Topic: "procurement"}),
		classifiedDoc("kept-1.txt", classifiedTopic{Title: "Procurement", Topic: "procurement"}),
		classifiedDoc("kept-2.txt", classifiedTopic{Title: "Procurement", Topic: "procurement"}),
	}}, nil)
	classifier.documentCalls = make(chan classifiedInput, 3)
	classifier.groupReport = []heuristic{{Name: "corroboration", Signal: "positive", Rating: "medium"}}
	classifier.groupCalls = make(chan groupCall, 1)
	srv := newMapleServer(&perDocumentControlledClassifier{
		controlledClassifier: classifier,
		reportsByID: map[string][]heuristic{
			"filtered.txt": {
				{Name: "emotive_language", Signal: "negative", Rating: "high"},
				{Name: "ideology", Signal: "negative", Rating: "high"},
			},
			"kept-1.txt": {
				{Name: "emotive_language", Signal: "negative", Rating: "low"},
				{Name: "ideology", Signal: "negative", Rating: "low"},
			},
			"kept-2.txt": {
				{Name: "emotive_language", Signal: "negative", Rating: "low"},
				{Name: "ideology", Signal: "negative", Rating: "low"},
			},
		},
	})

	caseID := postMapleCase(t, srv, []docInput{
		{Filename: "filtered.txt", Content: "filtered"},
		{Filename: "kept-1.txt", Content: "kept one"},
		{Filename: "kept-2.txt", Content: "kept two"},
	})
	waitMapleStatus(t, srv, caseID, statusComplete)

	select {
	case call := <-classifier.groupCalls:
		if got := len(call.documents); got != 2 {
			t.Fatalf("group call documents = %d, want 2", got)
		}
		for _, doc := range call.documents {
			if doc.ID == "filtered.txt" {
				t.Fatal("filtered document was included in group scan")
			}
		}
	default:
		t.Fatal("expected ClassifyGroup to be called")
	}
}

func TestProcessCaseSkipsGroupScanWhenFewerThanTwoDocumentsRemain(t *testing.T) {
	classifier := newControlledClassifier(classificationReport{Documents: []classifiedDocument{
		classifiedDoc("filtered.txt", classifiedTopic{Title: "Procurement", Topic: "procurement"}),
		classifiedDoc("kept.txt", classifiedTopic{Title: "Procurement", Topic: "procurement"}),
	}}, nil)
	classifier.groupCalls = make(chan groupCall, 1)
	srv := newMapleServer(&perDocumentControlledClassifier{
		controlledClassifier: classifier,
		reportsByID: map[string][]heuristic{
			"filtered.txt": {
				{Name: "emotive_language", Signal: "negative", Rating: "high"},
				{Name: "ideology", Signal: "negative", Rating: "high"},
			},
			"kept.txt": {
				{Name: "emotive_language", Signal: "negative", Rating: "low"},
				{Name: "ideology", Signal: "negative", Rating: "low"},
			},
		},
	})

	caseID := postMapleCase(t, srv, []docInput{
		{Filename: "filtered.txt", Content: "filtered"},
		{Filename: "kept.txt", Content: "kept"},
	})
	waitMapleStatus(t, srv, caseID, statusComplete)

	select {
	case <-classifier.groupCalls:
		t.Fatal("ClassifyGroup should not have been called")
	default:
	}
}

type perDocumentControlledClassifier struct {
	*controlledClassifier
	reportsByID map[string][]heuristic
	factsByID   map[string][]string
}

func (c *perDocumentControlledClassifier) ClassifyDocument(ctx context.Context, document classifiedInput) (documentClassification, error) {
	if c.documentCalls != nil {
		c.documentCalls <- document
	}
	if c.documentErr != nil {
		return documentClassification{}, c.documentErr
	}
	return documentClassification{
		Heuristics:    c.reportsByID[document.ID],
		FactsToVerify: c.factsByID[document.ID],
	}, nil
}

func containsHeuristicNamed(hs []heuristic, name string) bool {
	for _, h := range hs {
		if h.Name == name {
			return true
		}
	}
	return false
}

func containsTopicHeuristicNamed(hs []topicHeuristic, name string) bool {
	for _, h := range hs {
		if h.Name == name {
			return true
		}
	}
	return false
}

func heuristicNames(hs []heuristic) []string {
	out := make([]string, len(hs))
	for i, h := range hs {
		out[i] = h.Name
	}
	return out
}

func topicHeuristicNames(hs []topicHeuristic) []string {
	out := make([]string, len(hs))
	for i, h := range hs {
		out[i] = h.Name
	}
	return out
}

func assertNoGroupCall(t *testing.T, calls <-chan groupCall) {
	t.Helper()
	select {
	case <-calls:
		t.Fatal("ClassifyGroup was called before case and document stages completed")
	case <-time.After(25 * time.Millisecond):
	}
}

func TestMapleClassifierFailureMarksCaseFailed(t *testing.T) {
	classifier := newControlledClassifier(classificationReport{}, errors.New("maple unavailable"))
	srv := newMapleServer(classifier)

	caseID := postMapleCase(t, srv, []docInput{{Filename: "memo.txt", Content: "memo body"}})
	<-classifier.calls

	failed := waitMapleStatus(t, srv, caseID, statusFailed)
	if failed.Status != statusFailed {
		t.Fatalf("status = %q, want %q", failed.Status, statusFailed)
	}
}

func postMapleCase(t *testing.T, srv *mapleServer, docs []docInput) int {
	t.Helper()
	body, err := json.Marshal(createCaseRequest{Documents: docs})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cases", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.createCaseHandler(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /cases status = %d body = %s", rec.Code, rec.Body.String())
	}
	var resp createCaseResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	return resp.CaseID
}

func getMapleCase(t *testing.T, srv *mapleServer, caseID int) caseSummaryResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cases/"+strconv.Itoa(caseID), nil)
	req.SetPathValue("case_id", strconv.Itoa(caseID))
	rec := httptest.NewRecorder()
	srv.getCaseHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /cases/%d status = %d body = %s", caseID, rec.Code, rec.Body.String())
	}
	var resp caseSummaryResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	return resp
}

func getMapleTopic(t *testing.T, srv *mapleServer, caseID, topicID int) topicDetailResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cases/"+strconv.Itoa(caseID)+"/topics/"+strconv.Itoa(topicID), nil)
	req.SetPathValue("case_id", strconv.Itoa(caseID))
	req.SetPathValue("topic_id", strconv.Itoa(topicID))
	rec := httptest.NewRecorder()
	srv.getTopicHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET topic status = %d body = %s", rec.Code, rec.Body.String())
	}
	var resp topicDetailResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	return resp
}

func getMapleTopicDocuments(t *testing.T, srv *mapleServer, caseID, topicID int) topicDocumentsResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cases/"+strconv.Itoa(caseID)+"/topics/"+strconv.Itoa(topicID)+"/documents", nil)
	req.SetPathValue("case_id", strconv.Itoa(caseID))
	req.SetPathValue("topic_id", strconv.Itoa(topicID))
	rec := httptest.NewRecorder()
	srv.getTopicDocumentsHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET topic documents status = %d body = %s", rec.Code, rec.Body.String())
	}
	var resp topicDocumentsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	return resp
}

func waitMapleStatus(t *testing.T, srv *mapleServer, caseID int, want string) caseSummaryResponse {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		resp := getMapleCase(t, srv, caseID)
		if resp.Status == want {
			return resp
		}
		if time.Now().After(deadline) {
			t.Fatalf("case %d did not reach status=%q (last status=%q)", caseID, want, resp.Status)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
