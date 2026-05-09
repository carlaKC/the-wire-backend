package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
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
	documentReport   []heuristic
	documentErr      error
	documentReleases chan struct{}
	documentCalls    chan classifiedInput
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

func (c *controlledClassifier) ClassifyDocument(_ context.Context, document classifiedInput) ([]heuristic, error) {
	if c.documentCalls != nil {
		c.documentCalls <- document
	}
	if c.documentReleases != nil {
		<-c.documentReleases
	}
	return c.documentReport, c.documentErr
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
	classifier.documentReport = []heuristic{
		{Name: "consistency", Rating: "high", Description: "coherent timeline"},
		{Name: "references", Rating: "medium", Description: "PO referenced"},
		{Name: "emotive_language", Rating: "low", Description: "factual tone"},
		{Name: "ideology_or_incentives", Rating: "low", Description: "no agenda"},
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
	wantPromptNames := []string{"consistency", "references", "emotive_language", "ideology_or_incentives"}
	for _, name := range wantPromptNames {
		if !containsHeuristicNamed(got, name) {
			t.Errorf("document heuristics missing %q (from ClassifyDocument). got: %v", name, heuristicNames(got))
		}
	}
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
	classifier.documentReport = []heuristic{
		{Name: "consistency", Signal: "positive", Rating: "high", Description: "coherent"},
		{Name: "references", Signal: "positive", Rating: "medium", Description: "PO referenced"},
		{Name: "emotive_language", Signal: "negative", Rating: "low", Description: "factual"},
		{Name: "ideology_or_incentives", Signal: "negative", Rating: "low", Description: "no agenda"},
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

func containsHeuristicNamed(hs []heuristic, name string) bool {
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
