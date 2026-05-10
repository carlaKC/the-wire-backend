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

type topicBuild struct {
	id                    int
	title                 string
	description           string
	maxLevel              int
	documents             []documentResponse
	claimCount            int
	statuses              map[string]int
	docTypes              map[string]int
	groupKey              string
	importanceScore       string
	importanceExplanation string
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

func (s *classifiedCaseStore) replaceClassified(id int, createdAt time.Time, inputs []classifiedInput, report classificationReport, classificationsByDoc map[string]documentClassification, groupHeuristicsByKey map[string][]heuristic) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data := s.buildCaseDataLocked(createdAt, inputs, report, classificationsByDoc, groupHeuristicsByKey)
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

func (s *classifiedCaseStore) buildCaseDataLocked(createdAt time.Time, inputs []classifiedInput, report classificationReport, classificationsByDoc map[string]documentClassification, groupHeuristicsByKey map[string][]heuristic) caseData {
	inputByID := map[string]classifiedInput{}
	for _, input := range inputs {
		inputByID[input.ID] = input
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
				groupKey:    groupKeyForTopic(classified.Topic),
			}
			order = append(order, global.id)
		}

		topic := topics[global.id]
		score, explanation := classifiedImportanceRating(classified, topic.maxLevel)
		if ratingRank(score) > ratingRank(topic.importanceScore) {
			topic.importanceScore = score
			topic.importanceExplanation = explanation
		}
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
		docClassification := classificationsByDoc[classified.ID]
		topic.documents = append(topic.documents, documentResponse{
			ID:            nextDocumentID,
			Filename:      filename,
			Content:       input.Content,
			Filtered:      shouldFilterDocument(docClassification.Heuristics),
			Heuristics:    docClassification.Heuristics,
			FactsToVerify: docClassification.FactsToVerify,
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
		topicHs := topicHeuristics(topic.claimCount, topic.maxLevel, topic.statuses, topic.docTypes)
		if extras, ok := groupHeuristicsByKey[topic.groupKey]; ok {
			topicHs = append(topicHs, extras...)
		}
		triage := topicTriage(topic, topicHs)
		description := topicDescriptionWithTriage(topic.description, triage)

		summaries = append(summaries, topicSummary{
			ID:          topic.id,
			Title:       topic.title,
			Triage:      triage.Final,
			Description: description,
		})
		topicHs = append(topicHs, triageHeuristics(triage)...)

		details[topic.id] = topicDetailResponse{
			Topic: topicDetail{
				ID:            topic.id,
				Title:         topic.title,
				Triage:        triage.Final,
				Description:   description,
				DocumentCount: len(topic.documents),
				Heuristics:    topicHeuristicResponses(topicHs),
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

func topicTriage(topic *topicBuild, topicHeuristics []heuristic) triageRationale {
	importance := normalizedRating(topic.importanceScore)
	if importance == "" {
		importance = triageFromSensitivity(topic.maxLevel)
	}
	quality := evidenceQuality(topic.documents, topicHeuristics)
	final := finalTriage(importance, quality)
	description := topic.importanceExplanation
	if description == "" {
		description = "Importance falls back to the highest classified sensitivity in this topic."
	}
	return triageRationale{
		Importance:      importance,
		EvidenceQuality: quality,
		Final:           final,
		Description:     "Importance: " + description + " Evidence quality is " + quality + ".",
	}
}

func topicDescriptionWithTriage(description string, r triageRationale) string {
	description = strings.TrimSpace(description)
	note := "Triage: " + r.Final + " because importance is " + r.Importance + " and evidence quality is " + r.EvidenceQuality + "."
	if description == "" {
		return note
	}
	return description + " " + note
}

func triageHeuristics(r triageRationale) []heuristic {
	return []heuristic{
		{
			Name:        "importance",
			Signal:      "positive",
			Rating:      r.Importance,
			Description: r.Description,
		},
		{
			Name:        "evidence_quality",
			Signal:      "positive",
			Rating:      r.EvidenceQuality,
			Description: "Evidence quality used to gate final topic triage.",
		},
	}
}

func classifiedImportanceRating(document classifiedDocument, fallbackLevel int) (string, string) {
	score := normalizedRating(document.Topic.Importance.Score)
	if score != "" {
		return score, strings.TrimSpace(document.Topic.Importance.Explanation)
	}
	maxLevel := max(fallbackLevel, document.Sensitivity.Level)
	for _, claim := range document.Claims {
		maxLevel = max(maxLevel, claim.Sensitivity.Level)
	}
	return triageFromSensitivity(maxLevel), ""
}

func finalTriage(importance, quality string) string {
	switch normalizedRating(importance) {
	case "high":
		if normalizedRating(quality) == "low" {
			return "medium"
		}
		return "high"
	case "medium":
		if normalizedRating(quality) == "low" {
			return "low"
		}
		return "medium"
	default:
		return "low"
	}
}

func evidenceQuality(documents []documentResponse, topicHeuristics []heuristic) string {
	total := 0
	count := 0
	filtered := 0
	for _, document := range documents {
		if document.Filtered {
			filtered++
		}
		for _, h := range document.Heuristics {
			if score, ok := evidenceQualityScore(h); ok {
				total += score
				count++
			}
		}
	}
	for _, h := range topicHeuristics {
		if score, ok := evidenceQualityScore(h); ok {
			total += score
			count++
		}
	}
	if count == 0 {
		return "medium"
	}
	rating := ratingFromAverage(total, count)
	if hasHighContestedNarrative(topicHeuristics) && !hasStrongGroupSupport(topicHeuristics) && rating == "high" {
		rating = "medium"
	}
	if filtered == len(documents) && filtered > 0 {
		return "low"
	}
	if filtered > 0 && rating == "high" {
		return "medium"
	}
	return rating
}

func hasHighContestedNarrative(heuristics []heuristic) bool {
	for _, h := range heuristics {
		if normalizedTopic(h.Name) == "contested_narrative" && normalizedRating(h.Rating) == "high" {
			return true
		}
	}
	return false
}

func hasStrongGroupSupport(heuristics []heuristic) bool {
	for _, h := range heuristics {
		switch normalizedTopic(h.Name) {
		case "corroboration", "shared_references":
			if normalizedRating(h.Rating) == "high" {
				return true
			}
		}
	}
	return false
}

func evidenceQualityScore(h heuristic) (int, bool) {
	name := normalizedTopic(h.Name)
	switch name {
	case "consistency", "references", "corroboration", "shared_references", "timeline_coherence":
	case "emotive_language", "ideology", "coordinated_framing", "shared_agenda":
	default:
		return 0, false
	}
	rating := normalizedRating(h.Rating)
	if rating == "" {
		return 0, false
	}
	signal := normalizedTopic(h.Signal)
	if signal == "" {
		signal = heuristicSignal(name)
	}
	score := ratingRank(rating)
	if signal == "negative" {
		score = 4 - score
	}
	return score, true
}

func heuristicSignal(name string) string {
	switch normalizedTopic(name) {
	case "emotive_language", "ideology", "coordinated_framing", "shared_agenda":
		return "negative"
	default:
		return "positive"
	}
}

func ratingFromAverage(total, count int) string {
	avg := float64(total) / float64(count)
	if avg >= 2.5 {
		return "high"
	}
	if avg >= 1.5 {
		return "medium"
	}
	return "low"
}

func topicHeuristicResponses(heuristics []heuristic) []topicHeuristic {
	out := make([]topicHeuristic, 0, len(heuristics))
	for _, h := range heuristics {
		out = append(out, topicHeuristic{
			Name:        h.Name,
			Rating:      h.Rating,
			Description: h.Description,
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

func groupKeyForTopic(t classifiedTopic) string {
	if t.ID > 0 {
		return "id:" + strconv.Itoa(t.ID)
	}
	return "title:" + topicKey(groupTitleForTopic(t))
}

func groupTitleForTopic(t classifiedTopic) string {
	title := strings.TrimSpace(t.Title)
	if title == "" {
		title = humanTitle(t.Topic)
	}
	return title
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

func normalizedRating(value string) string {
	switch normalizedTopic(value) {
	case "high":
		return "high"
	case "medium":
		return "medium"
	case "low":
		return "low"
	default:
		return ""
	}
}

func ratingRank(value string) int {
	switch normalizedRating(value) {
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
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

func shouldFilterDocument(heuristics []heuristic) bool {
	negativeCount := 0
	for _, h := range heuristics {
		if normalizedTopic(h.Signal) != "negative" {
			continue
		}
		negativeCount++
		switch normalizedTopic(h.Rating) {
		case "medium", "high":
		default:
			return false
		}
	}
	return negativeCount > 0
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
