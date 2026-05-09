package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

type categorizationResult struct {
	Categories  []*cat
	Assignments map[int]int
}

func processCase(ctx context.Context, s *store, caseID int) {
	s.mu.RLock()
	k, ok := s.cases[caseID]
	if !ok {
		s.mu.RUnlock()
		return
	}
	docs := make([]*doc, len(k.Documents))
	copy(docs, k.Documents)
	s.mu.RUnlock()

	go runStage1(ctx, s, caseID, docs)
	go runStage2(ctx, s, caseID, docs)
}

func runStage1(ctx context.Context, s *store, caseID int, docs []*doc) {
	var wg sync.WaitGroup
	for _, d := range docs {
		wg.Add(1)
		go func(d *doc) {
			defer wg.Done()

			select {
			case s.docSem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-s.docSem }()

			hs, err := analyzeDocument(ctx, d, s.docDelay)
			if err != nil {
				return
			}
			s.recordDocHeuristics(caseID, d.ID, hs)
		}(d)
	}
	wg.Wait()
	s.markStageDone(caseID, 1)
}

func runStage2(ctx context.Context, s *store, caseID int, docs []*doc) {
	select {
	case s.caseSem <- struct{}{}:
	case <-ctx.Done():
		return
	}
	defer func() { <-s.caseSem }()

	result, err := categorize(ctx, docs, s.catDelay)
	if err != nil {
		s.markFailed(caseID, err.Error())
		return
	}
	s.recordCategorization(caseID, result.Categories, result.Assignments)
	s.markStageDone(caseID, 2)
}

func analyzeDocument(ctx context.Context, d *doc, delay time.Duration) ([]heuristic, error) {
	if err := sleepCtx(ctx, delay); err != nil {
		return nil, err
	}
	return []heuristic{
		{
			Name:        "consistency",
			Rating:      rateByLength(len(d.Content)),
			Description: fmt.Sprintf("Stub consistency rating derived from document length (%d chars).", len(d.Content)),
		},
		{
			Name:        "red_flags",
			Rating:      rateByKeyword(d.Content, "off-book", "confidential", "do not share"),
			Description: "Stub red-flag rating derived from suspicious keyword presence.",
		},
	}, nil
}

func categorize(ctx context.Context, docs []*doc, delay time.Duration) (categorizationResult, error) {
	if err := sleepCtx(ctx, delay); err != nil {
		return categorizationResult{}, err
	}
	if len(docs) == 0 {
		return categorizationResult{}, nil
	}
	n := len(docs)
	if n > 3 {
		n = 3
	}
	titles := []string{"Primary signals", "Supporting context", "Other"}
	descriptions := []string{
		"Documents the stub categorizer placed in the primary bucket.",
		"Documents the stub categorizer placed in the supporting bucket.",
		"Documents the stub categorizer did not match elsewhere.",
	}
	triages := []string{"high", "medium", "low"}
	cats := make([]*cat, n)
	for i := 0; i < n; i++ {
		cats[i] = &cat{
			ID:          i + 1,
			Title:       titles[i],
			Triage:      triages[i],
			Description: descriptions[i],
			Heuristics: []heuristic{
				{
					Name:        "consistency",
					Rating:      triages[i],
					Description: "Stub category-level consistency rating.",
				},
				{
					Name:        "red_flags",
					Rating:      triages[i],
					Description: "Stub category-level red-flag rating.",
				},
			},
		}
	}
	assignments := map[int]int{}
	for i, d := range docs {
		assignments[d.ID] = (i % n) + 1
	}
	return categorizationResult{Categories: cats, Assignments: assignments}, nil
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func rateByLength(n int) string {
	switch {
	case n < 200:
		return "low"
	case n < 1000:
		return "medium"
	default:
		return "high"
	}
}

func rateByKeyword(content string, keywords ...string) string {
	lower := strings.ToLower(content)
	count := 0
	for _, k := range keywords {
		if strings.Contains(lower, k) {
			count++
		}
	}
	switch count {
	case 0:
		return "low"
	case 1:
		return "medium"
	default:
		return "high"
	}
}
