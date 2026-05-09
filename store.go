package main

import (
	"errors"
	"sync"
	"time"
)

const (
	statusProcessing = "processing"
	statusComplete   = "complete"
	statusFailed     = "failed"
)

type doc struct {
	ID         int
	Filename   string
	Content    string
	Heuristics []heuristic
}

type cat struct {
	ID          int
	Title       string
	Triage      string
	Description string
	Heuristics  []heuristic
}

type kase struct {
	ID          int
	CreatedAt   time.Time
	Status      string
	ErrorMsg    string
	Documents   []*doc
	Categories  []*cat
	Assignments map[int]int

	stage1Done bool
	stage2Done bool
}

func (k *kase) advance() {
	if k.Status != statusProcessing {
		return
	}
	if k.stage1Done && k.stage2Done {
		k.Status = statusComplete
	}
}

type store struct {
	mu     sync.RWMutex
	cases  map[int]*kase
	nextID int

	docSem  chan struct{}
	caseSem chan struct{}

	docDelay  time.Duration
	catDelay  time.Duration
}

func newStore(docConcurrency, caseConcurrency int, docDelay, catDelay time.Duration) *store {
	return &store{
		cases:    map[int]*kase{},
		nextID:   1,
		docSem:   make(chan struct{}, docConcurrency),
		caseSem:  make(chan struct{}, caseConcurrency),
		docDelay: docDelay,
		catDelay: catDelay,
	}
}

func (s *store) createCase(inputs []docInput) *kase {
	s.mu.Lock()
	defer s.mu.Unlock()

	docs := make([]*doc, len(inputs))
	for i, in := range inputs {
		docs[i] = &doc{
			ID:         i + 1,
			Filename:   in.Filename,
			Content:    in.Content,
			Heuristics: []heuristic{},
		}
	}

	id := s.nextID
	s.nextID++

	k := &kase{
		ID:          id,
		CreatedAt:   time.Now().UTC(),
		Status:      statusProcessing,
		Documents:   docs,
		Categories:  nil,
		Assignments: map[int]int{},
	}
	s.cases[id] = k
	return k
}

var (
	errCaseNotFound     = errors.New("case not found")
	errCategoryNotFound = errors.New("category not found")
)

func (s *store) getCase(id int) (*kase, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	k, ok := s.cases[id]
	if !ok {
		return nil, errCaseNotFound
	}
	return k, nil
}

func (s *store) recordDocHeuristics(caseID, docID int, hs []heuristic) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k, ok := s.cases[caseID]
	if !ok {
		return
	}
	for _, d := range k.Documents {
		if d.ID == docID {
			d.Heuristics = hs
			return
		}
	}
}

func (s *store) recordCategorization(caseID int, cats []*cat, assignments map[int]int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k, ok := s.cases[caseID]
	if !ok {
		return
	}
	k.Categories = cats
	k.Assignments = assignments
}

func (s *store) markStageDone(caseID, stage int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k, ok := s.cases[caseID]
	if !ok {
		return
	}
	switch stage {
	case 1:
		k.stage1Done = true
	case 2:
		k.stage2Done = true
	}
	k.advance()
}

func (s *store) markFailed(caseID int, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k, ok := s.cases[caseID]
	if !ok {
		return
	}
	k.Status = statusFailed
	k.ErrorMsg = msg
}

func (s *store) caseSummary(id int) (caseSummaryResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	k, ok := s.cases[id]
	if !ok {
		return caseSummaryResponse{}, errCaseNotFound
	}
	summaries := make([]categorySummary, len(k.Categories))
	for i, c := range k.Categories {
		summaries[i] = categorySummary{
			ID:          c.ID,
			Title:       c.Title,
			Triage:      c.Triage,
			Description: c.Description,
		}
	}
	return caseSummaryResponse{
		CaseID:        k.ID,
		CreatedAt:     k.CreatedAt.Format(time.RFC3339),
		Status:        k.Status,
		DocumentCount: len(k.Documents),
		Categories:    summaries,
	}, nil
}

func (s *store) categoryDetail(caseID, categoryID int) (categoryDetailResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	k, ok := s.cases[caseID]
	if !ok {
		return categoryDetailResponse{}, errCaseNotFound
	}
	var found *cat
	for _, x := range k.Categories {
		if x.ID == categoryID {
			found = x
			break
		}
	}
	if found == nil {
		return categoryDetailResponse{}, errCategoryNotFound
	}
	docCount := 0
	for _, cid := range k.Assignments {
		if cid == categoryID {
			docCount++
		}
	}
	hs := append([]heuristic{}, found.Heuristics...)
	return categoryDetailResponse{
		CaseID: k.ID,
		Category: categoryDetail{
			ID:            found.ID,
			Title:         found.Title,
			Triage:        found.Triage,
			Description:   found.Description,
			DocumentCount: docCount,
			Heuristics:    hs,
		},
	}, nil
}

func (s *store) categoryDocuments(caseID, categoryID int) (categoryDocumentsResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	k, ok := s.cases[caseID]
	if !ok {
		return categoryDocumentsResponse{}, errCaseNotFound
	}
	exists := false
	for _, x := range k.Categories {
		if x.ID == categoryID {
			exists = true
			break
		}
	}
	if !exists {
		return categoryDocumentsResponse{}, errCategoryNotFound
	}
	docs := []documentResponse{}
	for _, d := range k.Documents {
		if k.Assignments[d.ID] != categoryID {
			continue
		}
		hs := append([]heuristic{}, d.Heuristics...)
		docs = append(docs, documentResponse{
			ID:         d.ID,
			Filename:   d.Filename,
			Content:    d.Content,
			Heuristics: hs,
		})
	}
	return categoryDocumentsResponse{
		CaseID:     k.ID,
		CategoryID: categoryID,
		Documents:  docs,
	}, nil
}
