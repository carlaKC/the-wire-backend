package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"
)

func newTestServer(t *testing.T, docDelay, catDelay time.Duration) *httptest.Server {
	t.Helper()
	s := newStore(8, 4, docDelay, catDelay)
	srv := &server{store: s, ctx: context.Background()}
	mux := http.NewServeMux()
	registerNomockHandlers(mux, srv)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func postCase(t *testing.T, ts *httptest.Server, docs []docInput) int {
	t.Helper()
	body, err := json.Marshal(createCaseRequest{Documents: docs})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(ts.URL+"/api/v1/cases", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /cases: %d %s", resp.StatusCode, b)
	}
	var got createCaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	return got.CaseID
}

func getCaseSummary(t *testing.T, ts *httptest.Server, caseID int) caseSummaryResponse {
	t.Helper()
	resp, err := http.Get(fmt.Sprintf("%s/api/v1/cases/%d", ts.URL, caseID))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET /cases/%d: %d %s", caseID, resp.StatusCode, b)
	}
	var got caseSummaryResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	return got
}

func waitForStatus(t *testing.T, ts *httptest.Server, caseID int, want string) caseSummaryResponse {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		s := getCaseSummary(t, ts, caseID)
		if s.Status == want {
			return s
		}
		if time.Now().After(deadline) {
			t.Fatalf("case %d did not reach status=%q (last status=%q)", caseID, want, s.Status)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestPostReturnsImmediately(t *testing.T) {
	ts := newTestServer(t, 200*time.Millisecond, 200*time.Millisecond)
	start := time.Now()
	caseID := postCase(t, ts, []docInput{{Filename: "a.txt", Content: "hi"}})
	elapsed := time.Since(start)
	if elapsed > 50*time.Millisecond {
		t.Errorf("POST took %v; expected to return well before processing finishes", elapsed)
	}
	if caseID < 1 {
		t.Errorf("expected positive caseID, got %d", caseID)
	}
}

func TestStatusTransitions(t *testing.T) {
	ts := newTestServer(t, 100*time.Millisecond, 100*time.Millisecond)
	caseID := postCase(t, ts, []docInput{
		{Filename: "a.txt", Content: "hi"},
		{Filename: "b.txt", Content: "bye"},
	})

	s := getCaseSummary(t, ts, caseID)
	if s.Status != statusProcessing {
		t.Errorf("expected status=%q immediately after POST, got %q", statusProcessing, s.Status)
	}
	if len(s.Categories) != 0 {
		t.Errorf("expected empty categories during processing, got %d", len(s.Categories))
	}
	if s.DocumentCount != 2 {
		t.Errorf("expected document_count=2 immediately, got %d", s.DocumentCount)
	}

	final := waitForStatus(t, ts, caseID, statusComplete)
	if len(final.Categories) == 0 {
		t.Errorf("expected categories after complete, got 0")
	}
	if final.DocumentCount != 2 {
		t.Errorf("expected document_count=2, got %d", final.DocumentCount)
	}
}

func TestCategoryEndpointReturns404DuringProcessing(t *testing.T) {
	ts := newTestServer(t, 300*time.Millisecond, 300*time.Millisecond)
	caseID := postCase(t, ts, []docInput{{Filename: "a.txt", Content: "hi"}})

	resp, err := http.Get(fmt.Sprintf("%s/api/v1/cases/%d/categories/1", ts.URL, caseID))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for category during processing, got %d", resp.StatusCode)
	}
}

func TestCategoryDocumentsAfterComplete(t *testing.T) {
	ts := newTestServer(t, 20*time.Millisecond, 20*time.Millisecond)
	caseID := postCase(t, ts, []docInput{
		{Filename: "memo.txt", Content: "this is a memo about off-book payments to atlas, marked confidential"},
		{Filename: "ok.txt", Content: "regular content"},
	})
	waitForStatus(t, ts, caseID, statusComplete)

	summary := getCaseSummary(t, ts, caseID)
	if len(summary.Categories) == 0 {
		t.Fatal("no categories after complete")
	}

	totalDocs := 0
	for _, c := range summary.Categories {
		resp, err := http.Get(fmt.Sprintf("%s/api/v1/cases/%d/categories/%d/documents", ts.URL, caseID, c.ID))
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET docs for cat %d: %d", c.ID, resp.StatusCode)
		}
		var docs categoryDocumentsResponse
		if err := json.NewDecoder(resp.Body).Decode(&docs); err != nil {
			resp.Body.Close()
			t.Fatal(err)
		}
		resp.Body.Close()
		totalDocs += len(docs.Documents)
		for _, d := range docs.Documents {
			if d.Content == "" {
				t.Errorf("doc %d missing content", d.ID)
			}
			if len(d.Heuristics) == 0 {
				t.Errorf("doc %d in cat %d missing heuristics after complete", d.ID, c.ID)
			}
		}
	}
	if totalDocs != 2 {
		t.Errorf("expected 2 total docs across categories, got %d", totalDocs)
	}
}

func TestUnknownCase404(t *testing.T) {
	ts := newTestServer(t, 0, 0)
	resp, err := http.Get(ts.URL + "/api/v1/cases/9999")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestStagesRunInParallel(t *testing.T) {
	docDelay := 200 * time.Millisecond
	catDelay := 200 * time.Millisecond
	ts := newTestServer(t, docDelay, catDelay)

	start := time.Now()
	caseID := postCase(t, ts, []docInput{{Filename: "a.txt", Content: "hi"}})
	waitForStatus(t, ts, caseID, statusComplete)
	elapsed := time.Since(start)

	if elapsed > docDelay+catDelay-50*time.Millisecond {
		t.Errorf("processing took %v; should be ~max(docDelay, catDelay) (=%v) since stages run in parallel, not the sum", elapsed, docDelay)
	}
}

func TestPerDocFanout(t *testing.T) {
	docDelay := 200 * time.Millisecond
	ts := newTestServer(t, docDelay, 10*time.Millisecond)

	start := time.Now()
	docs := make([]docInput, 4)
	for i := range docs {
		docs[i] = docInput{Filename: fmt.Sprintf("doc%d.txt", i), Content: fmt.Sprintf("content %d", i)}
	}
	caseID := postCase(t, ts, docs)
	waitForStatus(t, ts, caseID, statusComplete)
	elapsed := time.Since(start)

	if elapsed > 2*docDelay {
		t.Errorf("4 docs took %v; should be ~docDelay (=%v) since per-doc analysis fans out", elapsed, docDelay)
	}
}

func TestNoGoroutineLeak(t *testing.T) {
	ts := newTestServer(t, 5*time.Millisecond, 5*time.Millisecond)

	for i := 0; i < 3; i++ {
		caseID := postCase(t, ts, []docInput{
			{Filename: "a.txt", Content: "warm-up"},
			{Filename: "b.txt", Content: "warm-up"},
		})
		waitForStatus(t, ts, caseID, statusComplete)
	}
	time.Sleep(50 * time.Millisecond)
	baseline := runtime.NumGoroutine()

	for i := 0; i < 10; i++ {
		caseID := postCase(t, ts, []docInput{
			{Filename: "a.txt", Content: "hi"},
			{Filename: "b.txt", Content: "bye"},
			{Filename: "c.txt", Content: "ok"},
		})
		waitForStatus(t, ts, caseID, statusComplete)
	}
	time.Sleep(100 * time.Millisecond)

	leaked := runtime.NumGoroutine() - baseline
	if leaked > 2 {
		t.Errorf("goroutine count grew by %d after 10 cases (baseline=%d, current=%d)", leaked, baseline, runtime.NumGoroutine())
	}
}

func TestCaseConcurrencyBound(t *testing.T) {
	docDelay := 100 * time.Millisecond
	catDelay := 100 * time.Millisecond
	s := newStore(8, 2, docDelay, catDelay) // caseSem cap = 2
	srv := &server{store: s, ctx: context.Background()}
	mux := http.NewServeMux()
	registerNomockHandlers(mux, srv)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	start := time.Now()
	caseIDs := make([]int, 6)
	for i := range caseIDs {
		caseIDs[i] = postCase(t, ts, []docInput{{Filename: "a.txt", Content: "hi"}})
	}
	for _, id := range caseIDs {
		waitForStatus(t, ts, id, statusComplete)
	}
	elapsed := time.Since(start)

	// 6 cases, caseSem cap 2 → at least 3 batches × catDelay
	min := 3 * catDelay
	if elapsed < min-30*time.Millisecond {
		t.Errorf("6 cases with caseSem cap 2 finished in %v; expected ≥ %v (3 batches of stage 2)", elapsed, min)
	}
}
