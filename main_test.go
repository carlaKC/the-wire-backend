package main

import (
	"strings"
	"testing"
	"time"
)

func TestReplaceClassifiedReusesGlobalTopicAcrossCases(t *testing.T) {
	store := newClassifiedCaseStore()
	createdAt := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)

	caseOneID := store.create(emptyCaseData(createdAt, 1))
	store.replaceClassified(caseOneID, createdAt, []classifiedInput{
		{ID: "memo.txt", Filename: "memo.txt", Content: "Vendor Atlas lacked a purchase order."},
	}, classificationReport{Documents: []classifiedDocument{
		classifiedDoc("memo.txt", classifiedTopic{
			Title:       "Procurement",
			Description: "Vendor approval and purchasing issues.",
			Topic:       "procurement",
			Confidence:  0.95,
		}),
	}}, nil, nil)

	caseOne, ok := store.get(caseOneID)
	if !ok {
		t.Fatal("case one was not stored")
	}
	if got := len(caseOne.summary.Topics); got != 1 {
		t.Fatalf("case one topic count = %d, want 1", got)
	}
	topicID := caseOne.summary.Topics[0].ID
	if topicID == 0 {
		t.Fatal("case one topic id was not assigned")
	}

	candidates := store.topicCandidates()
	if got := len(candidates); got != 1 {
		t.Fatalf("candidate count = %d, want 1", got)
	}
	if candidates[0].ID != topicID {
		t.Fatalf("candidate id = %d, want %d", candidates[0].ID, topicID)
	}

	caseTwoID := store.create(emptyCaseData(createdAt, 1))
	store.replaceClassified(caseTwoID, createdAt, []classifiedInput{
		{ID: "invoice.txt", Filename: "invoice.txt", Content: "The Atlas invoice was missing a purchase order."},
	}, classificationReport{Documents: []classifiedDocument{
		classifiedDoc("invoice.txt", classifiedTopic{
			ID:         topicID,
			Title:      "Procurement",
			Topic:      "procurement",
			Confidence: 0.92,
		}),
	}}, nil, nil)

	caseTwo, ok := store.get(caseTwoID)
	if !ok {
		t.Fatal("case two was not stored")
	}
	if got := len(caseTwo.summary.Topics); got != 1 {
		t.Fatalf("case two topic count = %d, want 1", got)
	}
	if got := caseTwo.summary.Topics[0].ID; got != topicID {
		t.Fatalf("case two topic id = %d, want reused id %d", got, topicID)
	}

	caseOneDocs := caseOne.documents[topicID].Documents
	caseTwoDocs := caseTwo.documents[topicID].Documents
	if got := len(caseOneDocs); got != 1 {
		t.Fatalf("case one docs = %d, want 1", got)
	}
	if got := len(caseTwoDocs); got != 1 {
		t.Fatalf("case two docs = %d, want 1", got)
	}
	if caseOneDocs[0].Filename == caseTwoDocs[0].Filename {
		t.Fatalf("case-scoped document lists were not distinct: %q", caseOneDocs[0].Filename)
	}
}

func TestReplaceClassifiedFallsBackWhenModelReturnsUnknownTopicID(t *testing.T) {
	store := newClassifiedCaseStore()
	createdAt := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	caseID := store.create(emptyCaseData(createdAt, 1))

	store.replaceClassified(caseID, createdAt, []classifiedInput{
		{ID: "briefing.txt", Filename: "briefing.txt", Content: "Briefing on foreign lobbying activity."},
	}, classificationReport{Documents: []classifiedDocument{
		classifiedDoc("briefing.txt", classifiedTopic{
			ID:          99,
			Title:       "Foreign Influence",
			Description: "Foreign lobbying and influence issues.",
			Topic:       "foreign_influence",
			Confidence:  0.88,
		}),
	}}, nil, nil)

	data, ok := store.get(caseID)
	if !ok {
		t.Fatal("case was not stored")
	}
	if got := len(data.summary.Topics); got != 1 {
		t.Fatalf("topic count = %d, want 1", got)
	}
	topic := data.summary.Topics[0]
	if topic.ID == 99 {
		t.Fatal("unknown model topic id was accepted")
	}
	if topic.Title != "Foreign Influence" {
		t.Fatalf("topic title = %q, want Foreign Influence", topic.Title)
	}
}

func TestReplaceClassifiedMergesNewTopicsByTitle(t *testing.T) {
	store := newClassifiedCaseStore()
	createdAt := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	caseID := store.create(emptyCaseData(createdAt, 2))

	store.replaceClassified(caseID, createdAt, []classifiedInput{
		{ID: "one.txt", Filename: "one.txt", Content: "First procurement note."},
		{ID: "two.txt", Filename: "two.txt", Content: "Second procurement note."},
	}, classificationReport{Documents: []classifiedDocument{
		classifiedDoc("one.txt", classifiedTopic{Title: "Procurement", Description: "Purchasing issues.", Topic: "procurement"}),
		classifiedDoc("two.txt", classifiedTopic{Title: " procurement ", Description: "Vendor issues.", Topic: "procurement"}),
	}}, nil, nil)

	data, ok := store.get(caseID)
	if !ok {
		t.Fatal("case was not stored")
	}
	if got := len(data.summary.Topics); got != 1 {
		t.Fatalf("topic count = %d, want merged topic", got)
	}
	topicID := data.summary.Topics[0].ID
	if got := len(data.documents[topicID].Documents); got != 2 {
		t.Fatalf("merged topic document count = %d, want 2", got)
	}
}

func TestReplaceClassifiedMarksDocumentFilteredWhenAllNegativeHeuristicsAreMediumOrHigh(t *testing.T) {
	store := newClassifiedCaseStore()
	createdAt := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	caseID := store.create(emptyCaseData(createdAt, 1))

	store.replaceClassified(caseID, createdAt, []classifiedInput{
		{ID: "bad.txt", Filename: "bad.txt", Content: "Bad file."},
	}, classificationReport{Documents: []classifiedDocument{
		classifiedDoc("bad.txt", classifiedTopic{Title: "Procurement", Topic: "procurement"}),
	}}, map[string][]heuristic{
		"bad.txt": {
			{Name: "consistency", Signal: "positive", Rating: "low"},
			{Name: "references", Signal: "positive", Rating: "low"},
			{Name: "emotive_language", Signal: "negative", Rating: "medium"},
			{Name: "ideology", Signal: "negative", Rating: "high"},
		},
	}, nil)

	data, ok := store.get(caseID)
	if !ok {
		t.Fatal("case was not stored")
	}
	topicID := data.summary.Topics[0].ID
	doc := data.documents[topicID].Documents[0]
	if !doc.Filtered {
		t.Fatal("document filtered = false, want true")
	}
}

func TestTopicTriageUsesLLMImportanceAndEvidenceQuality(t *testing.T) {
	tests := []struct {
		name       string
		importance classifiedImportance
		heuristics []heuristic
		want       string
	}{
		{
			name:       "high importance with good quality stays high",
			importance: classifiedImportance{Score: "high", Explanation: "Payment concealment is important."},
			heuristics: []heuristic{
				{Name: "consistency", Signal: "positive", Rating: "high"},
				{Name: "references", Signal: "positive", Rating: "high"},
				{Name: "emotive_language", Signal: "negative", Rating: "low"},
				{Name: "ideology", Signal: "negative", Rating: "low"},
			},
			want: "high",
		},
		{
			name:       "high importance with poor quality is gated to medium",
			importance: classifiedImportance{Score: "high", Explanation: "Payment concealment is important."},
			heuristics: []heuristic{
				{Name: "consistency", Signal: "positive", Rating: "low"},
				{Name: "references", Signal: "positive", Rating: "low"},
				{Name: "emotive_language", Signal: "negative", Rating: "high"},
				{Name: "ideology", Signal: "negative", Rating: "high"},
			},
			want: "medium",
		},
		{
			name:       "medium importance with poor quality becomes low",
			importance: classifiedImportance{Score: "medium", Explanation: "Process issue."},
			heuristics: []heuristic{
				{Name: "consistency", Signal: "positive", Rating: "low"},
				{Name: "references", Signal: "positive", Rating: "low"},
				{Name: "emotive_language", Signal: "negative", Rating: "high"},
				{Name: "ideology", Signal: "negative", Rating: "high"},
			},
			want: "low",
		},
		{
			name:       "low importance stays low with good quality",
			importance: classifiedImportance{Score: "low", Explanation: "Minor issue."},
			heuristics: []heuristic{
				{Name: "consistency", Signal: "positive", Rating: "high"},
				{Name: "references", Signal: "positive", Rating: "high"},
				{Name: "emotive_language", Signal: "negative", Rating: "low"},
				{Name: "ideology", Signal: "negative", Rating: "low"},
			},
			want: "low",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newClassifiedCaseStore()
			createdAt := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
			caseID := store.create(emptyCaseData(createdAt, 1))
			topic := classifiedTopic{Title: "Procurement", Topic: "procurement", Importance: tt.importance}
			store.replaceClassified(caseID, createdAt, []classifiedInput{
				{ID: "memo.txt", Filename: "memo.txt", Content: "Memo body."},
			}, classificationReport{Documents: []classifiedDocument{
				classifiedDoc("memo.txt", topic),
			}}, map[string][]heuristic{"memo.txt": tt.heuristics}, nil)

			data, ok := store.get(caseID)
			if !ok {
				t.Fatal("case was not stored")
			}
			got := data.summary.Topics[0]
			if got.Triage != tt.want {
				t.Fatalf("triage = %q, want %q", got.Triage, tt.want)
			}
			if !strings.Contains(got.Description, "Triage: "+tt.want) {
				t.Fatalf("description does not include triage rationale: %q", got.Description)
			}
		})
	}
}

func TestTopicTriageFallsBackToSensitivityWhenImportanceMissing(t *testing.T) {
	store := newClassifiedCaseStore()
	createdAt := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	caseID := store.create(emptyCaseData(createdAt, 1))
	doc := classifiedDoc("memo.txt", classifiedTopic{Title: "Procurement", Topic: "procurement"})
	doc.Sensitivity = classifiedSensitivity{Level: 3, Label: "high", Confidence: 0.9}

	store.replaceClassified(caseID, createdAt, []classifiedInput{
		{ID: "memo.txt", Filename: "memo.txt", Content: "Memo body."},
	}, classificationReport{Documents: []classifiedDocument{doc}}, map[string][]heuristic{
		"memo.txt": {
			{Name: "consistency", Signal: "positive", Rating: "high"},
			{Name: "references", Signal: "positive", Rating: "high"},
			{Name: "emotive_language", Signal: "negative", Rating: "low"},
			{Name: "ideology", Signal: "negative", Rating: "low"},
		},
	}, nil)

	data, ok := store.get(caseID)
	if !ok {
		t.Fatal("case was not stored")
	}
	got := data.summary.Topics[0]
	if got.Triage != "high" {
		t.Fatalf("triage = %q, want high", got.Triage)
	}
}

func TestEvidenceQualityTreatsContestedNarrativeAsCautionNotFilter(t *testing.T) {
	documents := []documentResponse{
		{Heuristics: []heuristic{
			{Name: "consistency", Signal: "positive", Rating: "high"},
			{Name: "references", Signal: "positive", Rating: "high"},
			{Name: "emotive_language", Signal: "negative", Rating: "low"},
			{Name: "ideology", Signal: "negative", Rating: "low"},
		}},
		{Heuristics: []heuristic{
			{Name: "consistency", Signal: "positive", Rating: "high"},
			{Name: "references", Signal: "positive", Rating: "high"},
			{Name: "emotive_language", Signal: "negative", Rating: "low"},
			{Name: "ideology", Signal: "negative", Rating: "low"},
		}},
	}

	got := evidenceQuality(documents, []heuristic{
		{Name: "corroboration", Signal: "positive", Rating: "medium"},
		{Name: "shared_references", Signal: "positive", Rating: "medium"},
		{Name: "contested_narrative", Signal: "positive", Rating: "high"},
	})
	if got != "medium" {
		t.Fatalf("evidenceQuality with high contested narrative and weak support = %q, want medium", got)
	}

	got = evidenceQuality(documents, []heuristic{
		{Name: "corroboration", Signal: "positive", Rating: "high"},
		{Name: "shared_references", Signal: "positive", Rating: "medium"},
		{Name: "contested_narrative", Signal: "positive", Rating: "high"},
	})
	if got != "high" {
		t.Fatalf("evidenceQuality with high contested narrative and strong support = %q, want high", got)
	}
}

func TestContestedNarrativeDoesNotAffectDocumentFiltering(t *testing.T) {
	got := shouldFilterDocument([]heuristic{
		{Name: "contested_narrative", Signal: "positive", Rating: "high"},
	})
	if got {
		t.Fatal("contested_narrative should not filter a document")
	}
}

func TestTimelineHeuristicsEvidenceQualityAndFiltering(t *testing.T) {
	documents := []documentResponse{
		{Heuristics: []heuristic{
			{Name: "consistency", Signal: "positive", Rating: "medium"},
			{Name: "references", Signal: "positive", Rating: "medium"},
			{Name: "emotive_language", Signal: "negative", Rating: "low"},
			{Name: "ideology", Signal: "negative", Rating: "low"},
		}},
	}

	got := evidenceQuality(documents, []heuristic{
		{Name: "timeline_coherence", Signal: "positive", Rating: "high"},
	})
	if got != "high" {
		t.Fatalf("evidenceQuality with high timeline coherence = %q, want high", got)
	}

	got = evidenceQuality(documents, []heuristic{
		{Name: "timeline_coherence", Signal: "positive", Rating: "low"},
	})
	if got != "medium" {
		t.Fatalf("evidenceQuality with low timeline coherence = %q, want medium", got)
	}

	neutralDocuments := []documentResponse{
		{Heuristics: []heuristic{
			{Name: "consistency", Signal: "positive", Rating: "medium"},
			{Name: "references", Signal: "positive", Rating: "medium"},
			{Name: "emotive_language", Signal: "negative", Rating: "medium"},
			{Name: "ideology", Signal: "negative", Rating: "medium"},
		}},
	}

	got = evidenceQuality(neutralDocuments, []heuristic{
		{Name: "temporal_scope", Signal: "positive", Rating: "high"},
	})
	if got != "medium" {
		t.Fatalf("evidenceQuality with temporal scope only = %q, want medium", got)
	}

	if shouldFilterDocument([]heuristic{
		{Name: "timeline_coherence", Signal: "positive", Rating: "low"},
		{Name: "temporal_scope", Signal: "positive", Rating: "high"},
	}) {
		t.Fatal("timeline heuristics should not filter a document")
	}
}

func TestShouldFilterDocumentIgnoresPositiveHeuristics(t *testing.T) {
	tests := []struct {
		name       string
		heuristics []heuristic
		want       bool
	}{
		{
			name: "positive high does not filter when negative low",
			heuristics: []heuristic{
				{Name: "consistency", Signal: "positive", Rating: "high"},
				{Name: "references", Signal: "positive", Rating: "high"},
				{Name: "emotive_language", Signal: "negative", Rating: "low"},
				{Name: "ideology", Signal: "negative", Rating: "medium"},
			},
			want: false,
		},
		{
			name: "all negative medium or high filters",
			heuristics: []heuristic{
				{Name: "consistency", Signal: "positive", Rating: "low"},
				{Name: "references", Signal: "positive", Rating: "low"},
				{Name: "emotive_language", Signal: "negative", Rating: "medium"},
				{Name: "ideology", Signal: "negative", Rating: "high"},
			},
			want: true,
		},
		{
			name: "no negative signals does not filter",
			heuristics: []heuristic{
				{Name: "consistency", Signal: "positive", Rating: "high"},
				{Name: "references", Signal: "positive", Rating: "high"},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldFilterDocument(tt.heuristics); got != tt.want {
				t.Fatalf("shouldFilterDocument() = %v, want %v", got, tt.want)
			}
		})
	}
}

func classifiedDoc(id string, topic classifiedTopic) classifiedDocument {
	return classifiedDocument{
		ID:    id,
		Topic: topic,
		DocumentType: classifiedTopic{
			Topic:      "memo",
			Confidence: 0.8,
		},
		Sensitivity: classifiedSensitivity{Level: 2, Label: "sensitive", Confidence: 0.8},
		Rationale:   "Document matches the assigned topic.",
		Claims: []classifiedClaim{
			{
				ID:         "claim-1",
				Claim:      "A claim appears in the document.",
				Confidence: 0.8,
				Validation: classifiedValidation{
					Status:     "supported",
					Confidence: 0.8,
					Rationale:  "The document text supports the claim.",
				},
				Sensitivity: classifiedSensitivity{Level: 2, Label: "sensitive", Confidence: 0.8},
			},
		},
	}
}
