package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxConcurrentDocumentClassifications = 4

type mapleServer struct {
	store      *classifiedCaseStore
	classifier classifier
}

func newMapleServer(classifier classifier) *mapleServer {
	return &mapleServer{store: newClassifiedCaseStore(), classifier: classifier}
}

func registerMapleHandlers(mux *http.ServeMux, srv *mapleServer) {
	mux.HandleFunc("POST /api/v1/cases", srv.createCaseHandler)
	mux.HandleFunc("GET /api/v1/cases/{case_id}", srv.getCaseHandler)
	mux.HandleFunc("GET /api/v1/cases/{case_id}/categories/{category_id}", srv.getCategoryHandler)
	mux.HandleFunc("GET /api/v1/cases/{case_id}/categories/{category_id}/documents", srv.getCategoryDocumentsHandler)
}

func (s *mapleServer) createCaseHandler(w http.ResponseWriter, r *http.Request) {
	var req createCaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "request body is not valid JSON")
		return
	}
	if len(req.Documents) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "documents must contain at least one entry")
		return
	}

	documents, err := prepareClassifiedDocuments(req.Documents)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	createdAt := time.Now()
	caseID := s.store.create(emptyCaseData(createdAt, len(documents)))
	log.Printf("case %d: submitted (%d documents)", caseID, len(documents))
	go s.processCase(caseID, createdAt, documents)
	writeJSON(w, http.StatusCreated, createCaseResponse{CaseID: caseID})
}

// processCase coordinates the two case-processing pipelines. They are
// independent and run concurrently:
//
//   - classifyCase runs classification for the whole case (categorization +
//     per-document metadata via the model).
//   - classifyDocuments will run per-document analysis. It is currently a
//     no-op placeholder so the wiring exists for a future prompt.
//
// Both goroutines `defer wg.Done()` so a panic can't leak the WaitGroup, and
// processCase blocks on Wait before deciding the case's terminal state. A
// failure in either pipeline marks the whole case failed; only when both
// succeed does the classification report get committed.
func (s *mapleServer) processCase(caseID int, createdAt time.Time, documents []classifiedInput) {
	ctx := context.Background()
	start := time.Now()

	var (
		wg              sync.WaitGroup
		report          classificationReport
		classifyErr     error
		heuristicsByDoc map[string][]heuristic
		documentsErr    error
	)

	wg.Add(2)
	go func() {
		defer wg.Done()
		stageStart := time.Now()
		log.Printf("case %d: classifyCase started", caseID)
		report, classifyErr = s.classifyCase(ctx, documents)
		if classifyErr == nil {
			log.Printf("case %d: classifyCase completed in %s", caseID, time.Since(stageStart).Round(time.Millisecond))
		}
	}()
	go func() {
		defer wg.Done()
		stageStart := time.Now()
		log.Printf("case %d: classifyDocuments started", caseID)
		heuristicsByDoc, documentsErr = s.classifyDocuments(ctx, documents)
		if documentsErr == nil {
			log.Printf("case %d: classifyDocuments completed in %s (%d documents)", caseID, time.Since(stageStart).Round(time.Millisecond), len(documents))
		}
	}()
	wg.Wait()

	if classifyErr != nil {
		log.Printf("case %d: classifyCase failed: %v", caseID, classifyErr)
		s.store.markFailed(caseID)
		return
	}
	if documentsErr != nil {
		log.Printf("case %d: classifyDocuments failed: %v", caseID, documentsErr)
		s.store.markFailed(caseID)
		return
	}
	s.store.replaceClassified(caseID, createdAt, documents, report, heuristicsByDoc)
	log.Printf("case %d: complete (elapsed %s)", caseID, time.Since(start).Round(time.Millisecond))
}

// classifyCase runs the case-level classification pipeline: one model call
// against the full document set, returning categories and per-document
// metadata. The store-level classification candidates are passed in so the
// model can reuse category IDs across cases.
func (s *mapleServer) classifyCase(ctx context.Context, documents []classifiedInput) (classificationReport, error) {
	return s.classifier.Classify(ctx, documents, s.store.categoryCandidates())
}

// classifyDocuments fans out one ClassifyDocument call per document via the
// classifier interface. Calls run concurrently, bounded by a semaphore. The
// result map is keyed by classifiedInput.ID so the caller can attach each
// document's heuristics to its wire response.
//
// Per-document failures are collected and joined; any failure fails the
// whole stage (and so the whole case). This matches our policy elsewhere:
// document-level heuristics come from the model or not at all.
func (s *mapleServer) classifyDocuments(ctx context.Context, documents []classifiedInput) (map[string][]heuristic, error) {
	out := make(map[string][]heuristic, len(documents))
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	sem := make(chan struct{}, maxConcurrentDocumentClassifications)

	for _, d := range documents {
		wg.Add(1)
		go func(d classifiedInput) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				mu.Lock()
				errs = append(errs, fmt.Errorf("doc %s: %w", d.ID, ctx.Err()))
				mu.Unlock()
				return
			}
			defer func() { <-sem }()

			hs, err := s.classifier.ClassifyDocument(ctx, d)
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("doc %s: %w", d.ID, err))
				mu.Unlock()
				return
			}
			mu.Lock()
			out[d.ID] = hs
			mu.Unlock()
		}(d)
	}
	wg.Wait()

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return out, nil
}

func (s *mapleServer) getCaseHandler(w http.ResponseWriter, r *http.Request) {
	caseID, data, ok := s.lookupCase(w, r)
	if !ok {
		return
	}
	summary := data.summary
	summary.CaseID = caseID
	writeJSON(w, http.StatusOK, summary)
}

func (s *mapleServer) getCategoryHandler(w http.ResponseWriter, r *http.Request) {
	caseID, data, ok := s.lookupCase(w, r)
	if !ok {
		return
	}
	categoryID, ok := parseID(w, r, "category_id", "category_not_found")
	if !ok {
		return
	}
	resp, ok := data.details[categoryID]
	if !ok {
		writeError(w, http.StatusNotFound, "category_not_found", "no category with id "+strconv.Itoa(categoryID)+" in case "+strconv.Itoa(caseID))
		return
	}
	resp.CaseID = caseID
	writeJSON(w, http.StatusOK, resp)
}

func (s *mapleServer) getCategoryDocumentsHandler(w http.ResponseWriter, r *http.Request) {
	caseID, data, ok := s.lookupCase(w, r)
	if !ok {
		return
	}
	categoryID, ok := parseID(w, r, "category_id", "category_not_found")
	if !ok {
		return
	}
	resp, ok := data.documents[categoryID]
	if !ok {
		writeError(w, http.StatusNotFound, "category_not_found", "no category with id "+strconv.Itoa(categoryID)+" in case "+strconv.Itoa(caseID))
		return
	}
	resp.CaseID = caseID
	writeJSON(w, http.StatusOK, resp)
}

func (s *mapleServer) lookupCase(w http.ResponseWriter, r *http.Request) (int, caseData, bool) {
	caseID, ok := parseID(w, r, "case_id", "case_not_found")
	if !ok {
		return 0, caseData{}, false
	}
	data, ok := s.store.get(caseID)
	if !ok {
		writeError(w, http.StatusNotFound, "case_not_found", "no case with id "+strconv.Itoa(caseID))
		return 0, caseData{}, false
	}
	return caseID, data, true
}

func prepareClassifiedDocuments(inputs []docInput) ([]classifiedInput, error) {
	seen := map[string]bool{}
	out := make([]classifiedInput, 0, len(inputs))
	for i, input := range inputs {
		content := strings.TrimSpace(input.Content)
		if content == "" {
			return nil, fmt.Errorf("documents[%d].content is required", i)
		}

		id := strings.TrimSpace(input.Filename)
		if id == "" || seen[id] {
			id = "document-" + strconv.Itoa(i+1)
		}
		seen[id] = true

		out = append(out, classifiedInput{
			ID:       id,
			Filename: strings.TrimSpace(input.Filename),
			Content:  content,
		})
	}
	return out, nil
}
