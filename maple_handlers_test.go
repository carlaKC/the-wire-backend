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
	documents          []classifiedInput
	existingCategories []categoryCandidate
}

type controlledClassifier struct {
	report  classificationReport
	err     error
	release chan struct{}
	calls   chan classifierCall
}

func newControlledClassifier(report classificationReport, err error) *controlledClassifier {
	return &controlledClassifier{
		report: report,
		err:    err,
		calls:  make(chan classifierCall, 1),
	}
}

func (c *controlledClassifier) Classify(_ context.Context, documents []classifiedInput, existingCategories []categoryCandidate) (classificationReport, error) {
	c.calls <- classifierCall{
		documents:          append([]classifiedInput{}, documents...),
		existingCategories: append([]categoryCandidate{}, existingCategories...),
	}
	if c.release != nil {
		<-c.release
	}
	return c.report, c.err
}

func TestMapleCreateCaseStartsProcessingAndCompletes(t *testing.T) {
	classifier := newControlledClassifier(classificationReport{Documents: []classifiedDocument{
		classifiedDoc("memo.txt", classifiedCategory{
			Title:       "Procurement",
			Description: "Vendor approval and purchasing issues.",
			Category:    "procurement",
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
	if got := len(processing.Categories); got != 0 {
		t.Fatalf("initial category count = %d, want 0", got)
	}

	close(classifier.release)
	complete := waitMapleStatus(t, srv, caseID, statusComplete)
	if got := len(complete.Categories); got != 1 {
		t.Fatalf("complete category count = %d, want 1", got)
	}
	categoryID := complete.Categories[0].ID

	detail := getMapleCategory(t, srv, caseID, categoryID)
	if got := len(detail.Category.Heuristics); got == 0 {
		t.Fatal("category heuristics were empty")
	}
	docs := getMapleCategoryDocuments(t, srv, caseID, categoryID)
	if got := len(docs.Documents); got != 1 {
		t.Fatalf("category document count = %d, want 1", got)
	}
	if got := len(docs.Documents[0].Heuristics); got == 0 {
		t.Fatal("document heuristics were empty")
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

func getMapleCategory(t *testing.T, srv *mapleServer, caseID, categoryID int) categoryDetailResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cases/"+strconv.Itoa(caseID)+"/categories/"+strconv.Itoa(categoryID), nil)
	req.SetPathValue("case_id", strconv.Itoa(caseID))
	req.SetPathValue("category_id", strconv.Itoa(categoryID))
	rec := httptest.NewRecorder()
	srv.getCategoryHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET category status = %d body = %s", rec.Code, rec.Body.String())
	}
	var resp categoryDetailResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	return resp
}

func getMapleCategoryDocuments(t *testing.T, srv *mapleServer, caseID, categoryID int) categoryDocumentsResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cases/"+strconv.Itoa(caseID)+"/categories/"+strconv.Itoa(categoryID)+"/documents", nil)
	req.SetPathValue("case_id", strconv.Itoa(caseID))
	req.SetPathValue("category_id", strconv.Itoa(categoryID))
	rec := httptest.NewRecorder()
	srv.getCategoryDocumentsHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET category documents status = %d body = %s", rec.Code, rec.Body.String())
	}
	var resp categoryDocumentsResponse
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
