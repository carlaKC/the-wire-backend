package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
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
	mux.HandleFunc("GET /api/v1/cases/{case_id}/topics/{topic_id}", srv.getTopicHandler)
	mux.HandleFunc("GET /api/v1/cases/{case_id}/topics/{topic_id}/documents", srv.getTopicDocumentsHandler)
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

// processCase coordinates the case-processing pipeline:
//
//   - classifyCase runs classification for the whole case (topic assignment +
//     per-document metadata via the case-classification prompt).
//   - classifyDocuments fans out the document-heuristics prompt across each
//     document and collects per-document heuristic scores.
//   - classifyGroups runs one group-heuristics prompt for each topic with at
//     least two unfiltered documents.
//
// classifyCase and classifyDocuments run in parallel. classifyGroups starts
// only after both complete, because it needs topic assignments and
// per-document filter decisions.
//
// A failure in any stage marks the whole case failed; only when all stages
// succeed does the classification report get committed.
func (s *mapleServer) processCase(caseID int, createdAt time.Time, documents []classifiedInput) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	start := time.Now()

	type caseResult struct {
		report classificationReport
		err    error
	}
	type documentResult struct {
		heuristicsByDoc map[string][]heuristic
		err             error
	}

	caseDone := make(chan caseResult, 1)
	documentDone := make(chan documentResult, 1)

	go func() {
		stageStart := time.Now()
		log.Printf("case %d: classifyCase started", caseID)
		report, err := s.classifyCase(ctx, documents)
		if err != nil {
			log.Printf("case %d: classifyCase failed: %v", caseID, err)
			cancel()
			caseDone <- caseResult{err: err}
			return
		}
		log.Printf("case %d: classifyCase completed in %s", caseID, time.Since(stageStart).Round(time.Millisecond))
		caseDone <- caseResult{report: report}
	}()

	go func() {
		stageStart := time.Now()
		log.Printf("case %d: classifyDocuments started", caseID)
		heuristicsByDoc, err := s.classifyDocuments(ctx, documents)
		if err != nil {
			log.Printf("case %d: classifyDocuments failed: %v", caseID, err)
			cancel()
			documentDone <- documentResult{err: err}
			return
		}
		log.Printf("case %d: classifyDocuments completed in %s (%d documents)", caseID, time.Since(stageStart).Round(time.Millisecond), len(documents))
		documentDone <- documentResult{heuristicsByDoc: heuristicsByDoc}
	}()

	caseStage := <-caseDone
	documentStage := <-documentDone
	if caseStage.err != nil || documentStage.err != nil {
		s.store.markFailed(caseID)
		return
	}

	stageStart := time.Now()
	log.Printf("case %d: classifyGroups started", caseID)
	groupHeuristicsByKey, err := s.classifyGroups(ctx, documents, caseStage.report, documentStage.heuristicsByDoc)
	if err != nil {
		log.Printf("case %d: classifyGroups failed: %v", caseID, err)
		s.store.markFailed(caseID)
		return
	}
	log.Printf("case %d: classifyGroups completed in %s (%d groups)", caseID, time.Since(stageStart).Round(time.Millisecond), len(groupHeuristicsByKey))

	s.store.replaceClassified(caseID, createdAt, documents, caseStage.report, documentStage.heuristicsByDoc, groupHeuristicsByKey)
	log.Printf("case %d: complete (elapsed %s)", caseID, time.Since(start).Round(time.Millisecond))
}

// classifyCase runs the case-level classification pipeline: one model call
// against the full document set, returning topics and per-document
// metadata. The store-level classification candidates are passed in so the
// model can reuse topic IDs across cases.
func (s *mapleServer) classifyCase(ctx context.Context, documents []classifiedInput) (classificationReport, error) {
	return s.classifier.Classify(ctx, documents, s.store.topicCandidates())
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

// classifyGroups runs one ClassifyGroup call per case/topic bucket after the
// per-document filter decisions are known. Filtered documents are excluded; a
// bucket with fewer than two remaining documents is skipped.
func (s *mapleServer) classifyGroups(ctx context.Context, documents []classifiedInput, report classificationReport, heuristicsByDoc map[string][]heuristic) (map[string][]heuristic, error) {
	inputByID := map[string]classifiedInput{}
	for _, d := range documents {
		inputByID[d.ID] = d
	}

	type groupBucket struct {
		title     string
		documents []classifiedInput
	}
	groups := map[string]*groupBucket{}
	for _, classified := range report.Documents {
		input, ok := inputByID[classified.ID]
		if !ok || shouldFilterDocument(heuristicsByDoc[classified.ID]) {
			continue
		}

		key := groupKeyForTopic(classified.Topic)
		bucket, ok := groups[key]
		if !ok {
			bucket = &groupBucket{title: groupTitleForTopic(classified.Topic)}
			groups[key] = bucket
		}
		if bucket.title == "" {
			bucket.title = groupTitleForTopic(classified.Topic)
		}
		bucket.documents = append(bucket.documents, input)
	}

	out := make(map[string][]heuristic, len(groups))
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	sem := make(chan struct{}, maxConcurrentDocumentClassifications)

	for key, bucket := range groups {
		if len(bucket.documents) < 2 {
			continue
		}
		wg.Add(1)
		go func(key string, bucket *groupBucket) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				mu.Lock()
				errs = append(errs, fmt.Errorf("group %s: %w", key, ctx.Err()))
				mu.Unlock()
				return
			}
			defer func() { <-sem }()

			hs, err := s.classifier.ClassifyGroup(ctx, bucket.documents, bucket.title)
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("group %s: %w", key, err))
				mu.Unlock()
				return
			}
			mu.Lock()
			out[key] = hs
			mu.Unlock()
		}(key, bucket)
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

func (s *mapleServer) getTopicHandler(w http.ResponseWriter, r *http.Request) {
	caseID, data, ok := s.lookupCase(w, r)
	if !ok {
		return
	}
	topicID, ok := parseID(w, r, "topic_id", "topic_not_found")
	if !ok {
		return
	}
	resp, ok := data.details[topicID]
	if !ok {
		writeError(w, http.StatusNotFound, "topic_not_found", "no topic with id "+strconv.Itoa(topicID)+" in case "+strconv.Itoa(caseID))
		return
	}
	resp.CaseID = caseID
	writeJSON(w, http.StatusOK, resp)
}

// getTopicDocumentsHandler returns every document the case has classified
// under a single topic, with its content and heuristic scores. Documents are
// ordered by their stable per-case ID.
func (s *mapleServer) getTopicDocumentsHandler(w http.ResponseWriter, r *http.Request) {
	caseID, data, ok := s.lookupCase(w, r)
	if !ok {
		return
	}
	topicID, ok := parseID(w, r, "topic_id", "topic_not_found")
	if !ok {
		return
	}
	byTopic, ok := data.documents[topicID]
	if !ok {
		writeError(w, http.StatusNotFound, "topic_not_found", "no topic with id "+strconv.Itoa(topicID)+" in case "+strconv.Itoa(caseID))
		return
	}

	out := make([]documentResponse, len(byTopic.Documents))
	copy(out, byTopic.Documents)
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})

	writeJSON(w, http.StatusOK, topicDocumentsResponse{
		CaseID:    caseID,
		TopicID:   topicID,
		Documents: out,
	})
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
