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
	details   map[int]topicDetailResponse
	documents map[int]topicDocumentsResponse
}

type globalTopic struct {
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
	mu          sync.RWMutex
	nextCaseID  int
	nextTopicID int
	cases       map[int]caseData
	topics      map[int]*globalTopic
	topicTitles map[string]int
}

func newClassifiedCaseStore() *classifiedCaseStore {
	return &classifiedCaseStore{
		nextCaseID:  1,
		nextTopicID: 1,
		cases:       map[int]caseData{},
		topics:      map[int]*globalTopic{},
		topicTitles: map[string]int{},
	}
}

func (s *classifiedCaseStore) create(data caseData) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := s.nextCaseID
	s.nextCaseID++
	data.summary.CaseID = id
	for topicID, detail := range data.details {
		detail.CaseID = id
		data.details[topicID] = detail
	}
	for topicID, documents := range data.documents {
		documents.CaseID = id
		data.documents[topicID] = documents
	}
	s.cases[id] = data
	return id
}

func (s *classifiedCaseStore) replaceClassified(id int, createdAt time.Time, inputs []classifiedInput, report classificationReport, heuristicsByDoc map[string][]heuristic) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data := s.buildCaseDataLocked(createdAt, inputs, report, heuristicsByDoc)
	data.summary.CaseID = id
	for topicID, detail := range data.details {
		detail.CaseID = id
		data.details[topicID] = detail
	}
	for topicID, documents := range data.documents {
		documents.CaseID = id
		data.documents[topicID] = documents
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

func (s *classifiedCaseStore) topicCandidates() []topicCandidate {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]topicCandidate, 0, len(s.topics))
	for _, topic := range s.topics {
		out = append(out, topicCandidate{
			ID:          topic.id,
			Title:       topic.title,
			Description: topic.description,
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
			Topics:        []topicSummary{},
		},
		details:   map[int]topicDetailResponse{},
		documents: map[int]topicDocumentsResponse{},
	}
}

func (s *classifiedCaseStore) buildCaseDataLocked(createdAt time.Time, inputs []classifiedInput, report classificationReport, heuristicsByDoc map[string][]heuristic) caseData {
	inputByID := map[string]classifiedInput{}
	for _, input := range inputs {
		inputByID[input.ID] = input
	}

	type topicBuild struct {
		id          int
		title       string
		description string
		maxLevel    int
		documents   []documentResponse
		claimCount  int
		statuses    map[string]int
		docTypes    map[string]int
	}

	topics := map[int]*topicBuild{}
	order := []int{}
	nextDocumentID := 1

	for _, classified := range report.Documents {
		global := s.resolveTopicLocked(classified.Topic, classified.Rationale)
		s.updateGlobalTopicStatsLocked(global, classified)
		if _, ok := topics[global.id]; !ok {
			topics[global.id] = &topicBuild{
				id:          global.id,
				title:       global.title,
				description: global.description,
				maxLevel:    classified.Sensitivity.Level,
				statuses:    map[string]int{},
				docTypes:    map[string]int{},
			}
			order = append(order, global.id)
		}

		topic := topics[global.id]
		topic.maxLevel = max(topic.maxLevel, classified.Sensitivity.Level)
		topic.claimCount += len(classified.Claims)
		if classified.DocumentType.Topic != "" {
			topic.docTypes[normalizedTopic(classified.DocumentType.Topic)]++
		}
		for _, claim := range classified.Claims {
			status := validationStatus(claim.Validation.Status)
			topic.statuses[status]++
			topic.maxLevel = max(topic.maxLevel, claim.Sensitivity.Level)
		}

		input := inputByID[classified.ID]
		filename := input.Filename
		if filename == "" {
			filename = classified.ID
		}
		topic.documents = append(topic.documents, documentResponse{
			ID:         nextDocumentID,
			Filename:   filename,
			Content:    input.Content,
			Heuristics: heuristicsByDoc[classified.ID],
		})
		nextDocumentID++
	}

	summaries := make([]topicSummary, 0, len(order))
	details := map[int]topicDetailResponse{}
	documents := map[int]topicDocumentsResponse{}

	for _, id := range order {
		topic := topics[id]
		if topic.description == "" {
			topic.description = fmt.Sprintf("%d document(s) grouped under topic %s.", len(topic.documents), topic.title)
		}
		triage := triageFromSensitivity(topic.maxLevel)

		summaries = append(summaries, topicSummary{
			ID:          topic.id,
			Title:       topic.title,
			Triage:      triage,
			Description: topic.description,
		})

		details[topic.id] = topicDetailResponse{
			Topic: topicDetail{
				ID:            topic.id,
				Title:         topic.title,
				Triage:        triage,
				Description:   topic.description,
				DocumentCount: len(topic.documents),
				Heuristics:    topicHeuristics(topic.claimCount, topic.maxLevel, topic.statuses, topic.docTypes),
			},
		}
		documents[topic.id] = topicDocumentsResponse{
			TopicID:   topic.id,
			Documents: topic.documents,
		}
	}

	return caseData{
		summary: caseSummaryResponse{
			CreatedAt:     createdAt.UTC().Format(time.RFC3339),
			Status:        statusComplete,
			DocumentCount: len(inputs),
			Topics:        summaries,
		},
		details:   details,
		documents: documents,
	}
}

func (s *classifiedCaseStore) resolveTopicLocked(topic classifiedTopic, fallbackDescription string) *globalTopic {
	if topic.ID > 0 {
		if existing, ok := s.topics[topic.ID]; ok {
			return existing
		}
	}

	title := strings.TrimSpace(topic.Title)
	if title == "" {
		title = humanTitle(topic.Topic)
	}
	key := topicKey(title)
	if id, ok := s.topicTitles[key]; ok {
		return s.topics[id]
	}

	description := strings.TrimSpace(topic.Description)
	if description == "" {
		description = strings.TrimSpace(fallbackDescription)
	}
	if description == "" {
		description = "Documents grouped under topic " + title + "."
	}

	created := &globalTopic{
		id:          s.nextTopicID,
		title:       title,
		description: description,
		statuses:    map[string]int{},
		docTypes:    map[string]int{},
	}
	s.nextTopicID++
	s.topics[created.id] = created
	s.topicTitles[key] = created.id
	return created
}

func (s *classifiedCaseStore) updateGlobalTopicStatsLocked(topic *globalTopic, document classifiedDocument) {
	topic.documentCount++
	topic.maxLevel = max(topic.maxLevel, document.Sensitivity.Level)
	topic.claimCount += len(document.Claims)
	if document.DocumentType.Topic != "" {
		topic.docTypes[normalizedTopic(document.DocumentType.Topic)]++
	}
	for _, claim := range document.Claims {
		status := validationStatus(claim.Validation.Status)
		topic.statuses[status]++
		topic.maxLevel = max(topic.maxLevel, claim.Sensitivity.Level)
	}
}

func topicHeuristics(claimCount, maxSensitivity int, statuses map[string]int, docTypes map[string]int) []heuristic {
	out := []heuristic{
		{
			Name:        "sensitivity",
			Rating:      triageFromSensitivity(maxSensitivity),
			Description: "Highest sensitivity level in this topic is " + strconv.Itoa(maxSensitivity) + ".",
		},
		{
			Name:        "claims",
			Rating:      ratingFromClaimCount(claimCount),
			Description: strconv.Itoa(claimCount) + " factual claim(s) extracted in this topic.",
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

func normalizedTopic(topic string) string {
	topic = strings.TrimSpace(strings.ToLower(topic))
	if topic == "" {
		return "other"
	}
	return topic
}

func topicKey(title string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(title))), " ")
}

func humanTitle(value string) string {
	value = strings.ReplaceAll(normalizedTopic(value), "_", " ")
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
