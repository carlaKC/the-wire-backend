package main

import (
	"testing"
	"time"
)

func TestReplaceClassifiedReusesGlobalCategoryAcrossCases(t *testing.T) {
	store := newClassifiedCaseStore()
	createdAt := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)

	caseOneID := store.create(emptyCaseData(createdAt, 1))
	store.replaceClassified(caseOneID, createdAt, []classifiedInput{
		{ID: "memo.txt", Filename: "memo.txt", Content: "Vendor Atlas lacked a purchase order."},
	}, classificationReport{Documents: []classifiedDocument{
		classifiedDoc("memo.txt", classifiedCategory{
			Title:       "Procurement",
			Description: "Vendor approval and purchasing issues.",
			Category:    "procurement",
			Confidence:  0.95,
		}),
	}})

	caseOne, ok := store.get(caseOneID)
	if !ok {
		t.Fatal("case one was not stored")
	}
	if got := len(caseOne.summary.Categories); got != 1 {
		t.Fatalf("case one category count = %d, want 1", got)
	}
	categoryID := caseOne.summary.Categories[0].ID
	if categoryID == 0 {
		t.Fatal("case one category id was not assigned")
	}

	candidates := store.categoryCandidates()
	if got := len(candidates); got != 1 {
		t.Fatalf("candidate count = %d, want 1", got)
	}
	if candidates[0].ID != categoryID {
		t.Fatalf("candidate id = %d, want %d", candidates[0].ID, categoryID)
	}

	caseTwoID := store.create(emptyCaseData(createdAt, 1))
	store.replaceClassified(caseTwoID, createdAt, []classifiedInput{
		{ID: "invoice.txt", Filename: "invoice.txt", Content: "The Atlas invoice was missing a purchase order."},
	}, classificationReport{Documents: []classifiedDocument{
		classifiedDoc("invoice.txt", classifiedCategory{
			ID:         categoryID,
			Title:      "Procurement",
			Category:   "procurement",
			Confidence: 0.92,
		}),
	}})

	caseTwo, ok := store.get(caseTwoID)
	if !ok {
		t.Fatal("case two was not stored")
	}
	if got := len(caseTwo.summary.Categories); got != 1 {
		t.Fatalf("case two category count = %d, want 1", got)
	}
	if got := caseTwo.summary.Categories[0].ID; got != categoryID {
		t.Fatalf("case two category id = %d, want reused id %d", got, categoryID)
	}

	caseOneDocs := caseOne.documents[categoryID].Documents
	caseTwoDocs := caseTwo.documents[categoryID].Documents
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

func TestReplaceClassifiedFallsBackWhenModelReturnsUnknownCategoryID(t *testing.T) {
	store := newClassifiedCaseStore()
	createdAt := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	caseID := store.create(emptyCaseData(createdAt, 1))

	store.replaceClassified(caseID, createdAt, []classifiedInput{
		{ID: "briefing.txt", Filename: "briefing.txt", Content: "Briefing on foreign lobbying activity."},
	}, classificationReport{Documents: []classifiedDocument{
		classifiedDoc("briefing.txt", classifiedCategory{
			ID:          99,
			Title:       "Foreign Influence",
			Description: "Foreign lobbying and influence issues.",
			Category:    "foreign_influence",
			Confidence:  0.88,
		}),
	}})

	data, ok := store.get(caseID)
	if !ok {
		t.Fatal("case was not stored")
	}
	if got := len(data.summary.Categories); got != 1 {
		t.Fatalf("category count = %d, want 1", got)
	}
	category := data.summary.Categories[0]
	if category.ID == 99 {
		t.Fatal("unknown model category id was accepted")
	}
	if category.Title != "Foreign Influence" {
		t.Fatalf("category title = %q, want Foreign Influence", category.Title)
	}
}

func TestReplaceClassifiedMergesNewCategoriesByTitle(t *testing.T) {
	store := newClassifiedCaseStore()
	createdAt := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	caseID := store.create(emptyCaseData(createdAt, 2))

	store.replaceClassified(caseID, createdAt, []classifiedInput{
		{ID: "one.txt", Filename: "one.txt", Content: "First procurement note."},
		{ID: "two.txt", Filename: "two.txt", Content: "Second procurement note."},
	}, classificationReport{Documents: []classifiedDocument{
		classifiedDoc("one.txt", classifiedCategory{Title: "Procurement", Description: "Purchasing issues.", Category: "procurement"}),
		classifiedDoc("two.txt", classifiedCategory{Title: " procurement ", Description: "Vendor issues.", Category: "procurement"}),
	}})

	data, ok := store.get(caseID)
	if !ok {
		t.Fatal("case was not stored")
	}
	if got := len(data.summary.Categories); got != 1 {
		t.Fatalf("category count = %d, want merged category", got)
	}
	categoryID := data.summary.Categories[0].ID
	if got := len(data.documents[categoryID].Documents); got != 2 {
		t.Fatalf("merged category document count = %d, want 2", got)
	}
}

func classifiedDoc(id string, topic classifiedCategory) classifiedDocument {
	return classifiedDocument{
		ID:    id,
		Topic: topic,
		DocumentType: classifiedCategory{
			Category:   "memo",
			Confidence: 0.8,
		},
		Sensitivity: classifiedSensitivity{Level: 2, Label: "sensitive", Confidence: 0.8},
		Rationale:   "Document matches the assigned category.",
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
