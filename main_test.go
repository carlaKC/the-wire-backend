package main

import (
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
	}}, nil)

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
	}}, nil)

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
	}}, nil)

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
	}}, nil)

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
			{Name: "ideology_or_incentives", Signal: "negative", Rating: "high"},
		},
	})

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
				{Name: "ideology_or_incentives", Signal: "negative", Rating: "medium"},
			},
			want: false,
		},
		{
			name: "all negative medium or high filters",
			heuristics: []heuristic{
				{Name: "consistency", Signal: "positive", Rating: "low"},
				{Name: "references", Signal: "positive", Rating: "low"},
				{Name: "emotive_language", Signal: "negative", Rating: "medium"},
				{Name: "ideology_or_incentives", Signal: "negative", Rating: "high"},
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
