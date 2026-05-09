package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type caseData struct {
	summary   caseSummaryResponse
	details   map[int]categoryDetailResponse
	documents map[int]categoryDocumentsResponse
}

type globalCategory struct {
	id            int
	title         string
	description   string
	maxLevel      int
	documentCount int
	claimCount    int
	statuses      map[string]int
	docTypes      map[string]int
}

type classifiedCaseStore struct {
	mu             sync.RWMutex
	nextCaseID     int
	nextCategoryID int
	cases          map[int]caseData
	categories     map[int]*globalCategory
	categoryTitles map[string]int
}

func newClassifiedCaseStore() *classifiedCaseStore {
	return &classifiedCaseStore{
		nextCaseID:     1,
		nextCategoryID: 1,
		cases:          map[int]caseData{},
		categories:     map[int]*globalCategory{},
		categoryTitles: map[string]int{},
	}
}

func (s *classifiedCaseStore) create(data caseData) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := s.nextCaseID
	s.nextCaseID++
	data.summary.CaseID = id
	for categoryID, detail := range data.details {
		detail.CaseID = id
		data.details[categoryID] = detail
	}
	for categoryID, documents := range data.documents {
		documents.CaseID = id
		data.documents[categoryID] = documents
	}
	s.cases[id] = data
	return id
}

func (s *classifiedCaseStore) replaceClassified(id int, createdAt time.Time, inputs []classifiedInput, report classificationReport) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data := s.buildCaseDataLocked(createdAt, inputs, report)
	data.summary.CaseID = id
	for categoryID, detail := range data.details {
		detail.CaseID = id
		data.details[categoryID] = detail
	}
	for categoryID, documents := range data.documents {
		documents.CaseID = id
		data.documents[categoryID] = documents
	}
	s.cases[id] = data
}

func (s *classifiedCaseStore) markFailed(id int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.cases[id]
	if !ok {
		return
	}
	data.summary.Status = statusFailed
	s.cases[id] = data
}

func (s *classifiedCaseStore) categoryCandidates() []categoryCandidate {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]categoryCandidate, 0, len(s.categories))
	for _, category := range s.categories {
		out = append(out, categoryCandidate{
			ID:          category.id,
			Title:       category.title,
			Description: category.description,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

func (s *classifiedCaseStore) get(id int) (caseData, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, ok := s.cases[id]
	return data, ok
}

func emptyCaseData(createdAt time.Time, documentCount int) caseData {
	return caseData{
		summary: caseSummaryResponse{
			CreatedAt:     createdAt.UTC().Format(time.RFC3339),
			Status:        statusProcessing,
			DocumentCount: documentCount,
			Categories:    []categorySummary{},
		},
		details:   map[int]categoryDetailResponse{},
		documents: map[int]categoryDocumentsResponse{},
	}
}

func (s *classifiedCaseStore) buildCaseDataLocked(createdAt time.Time, inputs []classifiedInput, report classificationReport) caseData {
	inputByID := map[string]classifiedInput{}
	for _, input := range inputs {
		inputByID[input.ID] = input
	}

	type categoryBuild struct {
		id          int
		title       string
		description string
		maxLevel    int
		documents   []documentResponse
		claimCount  int
		statuses    map[string]int
		docTypes    map[string]int
	}

	categories := map[int]*categoryBuild{}
	order := []int{}
	nextDocumentID := 1

	for _, classified := range report.Documents {
		global := s.resolveCategoryLocked(classified.Topic, classified.Rationale)
		s.updateGlobalCategoryStatsLocked(global, classified)
		if _, ok := categories[global.id]; !ok {
			categories[global.id] = &categoryBuild{
				id:          global.id,
				title:       global.title,
				description: global.description,
				maxLevel:    classified.Sensitivity.Level,
				statuses:    map[string]int{},
				docTypes:    map[string]int{},
			}
			order = append(order, global.id)
		}

		category := categories[global.id]
		category.maxLevel = max(category.maxLevel, classified.Sensitivity.Level)
		category.claimCount += len(classified.Claims)
		if classified.DocumentType.Category != "" {
			category.docTypes[normalizedCategory(classified.DocumentType.Category)]++
		}
		for _, claim := range classified.Claims {
			status := validationStatus(claim.Validation.Status)
			category.statuses[status]++
			category.maxLevel = max(category.maxLevel, claim.Sensitivity.Level)
		}

		input := inputByID[classified.ID]
		filename := input.Filename
		if filename == "" {
			filename = classified.ID
		}
		category.documents = append(category.documents, documentResponse{
			ID:         nextDocumentID,
			Filename:   filename,
			Content:    input.Content,
			Heuristics: documentHeuristics(classified),
		})
		nextDocumentID++
	}

	summaries := make([]categorySummary, 0, len(order))
	details := map[int]categoryDetailResponse{}
	documents := map[int]categoryDocumentsResponse{}

	for _, id := range order {
		category := categories[id]
		if category.description == "" {
			category.description = fmt.Sprintf("%d document(s) categorized as %s.", len(category.documents), category.title)
		}
		triage := triageFromSensitivity(category.maxLevel)

		summaries = append(summaries, categorySummary{
			ID:          category.id,
			Title:       category.title,
			Triage:      triage,
			Description: category.description,
		})

		details[category.id] = categoryDetailResponse{
			Category: categoryDetail{
				ID:            category.id,
				Title:         category.title,
				Triage:        triage,
				Description:   category.description,
				DocumentCount: len(category.documents),
				Heuristics:    categoryHeuristics(category.claimCount, category.maxLevel, category.statuses, category.docTypes),
			},
		}
		documents[category.id] = categoryDocumentsResponse{
			CategoryID: category.id,
			Documents:  category.documents,
		}
	}

	return caseData{
		summary: caseSummaryResponse{
			CreatedAt:     createdAt.UTC().Format(time.RFC3339),
			Status:        statusComplete,
			DocumentCount: len(inputs),
			Categories:    summaries,
		},
		details:   details,
		documents: documents,
	}
}

func (s *classifiedCaseStore) resolveCategoryLocked(topic classifiedCategory, fallbackDescription string) *globalCategory {
	if topic.ID > 0 {
		if category, ok := s.categories[topic.ID]; ok {
			return category
		}
	}

	title := strings.TrimSpace(topic.Title)
	if title == "" {
		title = humanTitle(topic.Category)
	}
	key := categoryKey(title)
	if id, ok := s.categoryTitles[key]; ok {
		return s.categories[id]
	}

	description := strings.TrimSpace(topic.Description)
	if description == "" {
		description = strings.TrimSpace(fallbackDescription)
	}
	if description == "" {
		description = "Documents categorized as " + title + "."
	}

	category := &globalCategory{
		id:          s.nextCategoryID,
		title:       title,
		description: description,
		statuses:    map[string]int{},
		docTypes:    map[string]int{},
	}
	s.nextCategoryID++
	s.categories[category.id] = category
	s.categoryTitles[key] = category.id
	return category
}

func (s *classifiedCaseStore) updateGlobalCategoryStatsLocked(category *globalCategory, document classifiedDocument) {
	category.documentCount++
	category.maxLevel = max(category.maxLevel, document.Sensitivity.Level)
	category.claimCount += len(document.Claims)
	if document.DocumentType.Category != "" {
		category.docTypes[normalizedCategory(document.DocumentType.Category)]++
	}
	for _, claim := range document.Claims {
		status := validationStatus(claim.Validation.Status)
		category.statuses[status]++
		category.maxLevel = max(category.maxLevel, claim.Sensitivity.Level)
	}
}

func documentHeuristics(document classifiedDocument) []heuristic {
	out := []heuristic{}
	if document.Rationale != "" {
		out = append(out, heuristic{
			Name:        "classification_rationale",
			Rating:      triageFromSensitivity(document.Sensitivity.Level),
			Description: document.Rationale,
		})
	}
	if document.DocumentType.Category != "" {
		out = append(out, heuristic{
			Name:        "document_type",
			Rating:      ratingFromConfidence(document.DocumentType.Confidence),
			Description: "Classified as " + humanTitle(document.DocumentType.Category) + ".",
		})
	}
	for _, claim := range document.Claims {
		description := claim.Claim
		if claim.Evidence != "" {
			description += " Evidence: " + claim.Evidence
		}
		if claim.Validation.Rationale != "" {
			description += " Validation: " + claim.Validation.Rationale
		}
		out = append(out, heuristic{
			Name:        "claim_" + validationStatus(claim.Validation.Status),
			Rating:      ratingFromValidation(claim.Validation.Status),
			Description: description,
		})
	}
	if len(out) == 0 {
		out = append(out, heuristic{
			Name:        "classification",
			Rating:      triageFromSensitivity(document.Sensitivity.Level),
			Description: "Document was classified without additional rationale.",
		})
	}
	return out
}

func categoryHeuristics(claimCount, maxSensitivity int, statuses map[string]int, docTypes map[string]int) []heuristic {
	out := []heuristic{
		{
			Name:        "sensitivity",
			Rating:      triageFromSensitivity(maxSensitivity),
			Description: "Highest sensitivity level in this category is " + strconv.Itoa(maxSensitivity) + ".",
		},
		{
			Name:        "claims",
			Rating:      ratingFromClaimCount(claimCount),
			Description: strconv.Itoa(claimCount) + " factual claim(s) extracted in this category.",
		},
	}
	if len(statuses) > 0 {
		out = append(out, heuristic{
			Name:        "validation",
			Rating:      ratingFromValidationMix(statuses),
			Description: "Claim validation statuses: " + joinCounts(statuses) + ".",
		})
	}
	if len(docTypes) > 0 {
		out = append(out, heuristic{
			Name:        "document_types",
			Rating:      "medium",
			Description: "Document types: " + joinCounts(docTypes) + ".",
		})
	}
	return out
}

func normalizedCategory(category string) string {
	category = strings.TrimSpace(strings.ToLower(category))
	if category == "" {
		return "other"
	}
	return category
}

func categoryKey(title string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(title))), " ")
}

func humanTitle(value string) string {
	value = strings.ReplaceAll(normalizedCategory(value), "_", " ")
	words := strings.Fields(value)
	for i, word := range words {
		if len(word) == 0 {
			continue
		}
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	if len(words) == 0 {
		return "Other"
	}
	return strings.Join(words, " ")
}

func triageFromSensitivity(level int) string {
	switch {
	case level >= 3:
		return "high"
	case level == 2:
		return "medium"
	default:
		return "low"
	}
}

func ratingFromConfidence(confidence float64) string {
	switch {
	case confidence >= 0.75:
		return "high"
	case confidence >= 0.4:
		return "medium"
	default:
		return "low"
	}
}

func validationStatus(status string) string {
	status = strings.TrimSpace(strings.ToLower(status))
	if status == "" {
		return "unclear"
	}
	return status
}

func ratingFromValidation(status string) string {
	switch validationStatus(status) {
	case "supported":
		return "high"
	case "contradicted":
		return "low"
	default:
		return "medium"
	}
}

func ratingFromValidationMix(statuses map[string]int) string {
	if statuses["contradicted"] > 0 {
		return "low"
	}
	if statuses["unclear"] > 0 {
		return "medium"
	}
	return "high"
}

func ratingFromClaimCount(count int) string {
	if count >= 5 {
		return "high"
	}
	if count > 0 {
		return "medium"
	}
	return "low"
}

func joinCounts(counts map[string]int) string {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+strconv.Itoa(counts[key]))
	}
	return strings.Join(parts, ", ")
}
