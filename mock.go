package main

import (
	"encoding/json"
	"net/http"
	"strconv"
)

const (
	mockCaseID    = 1
	mockCreatedAt = "2026-05-09T12:34:56Z"
)

var mockCaseSummary = caseSummaryResponse{
	CaseID:        mockCaseID,
	CreatedAt:     mockCreatedAt,
	Status:        "complete",
	DocumentCount: 4,
	Topics: []topicSummary{
		{
			ID:          1,
			Title:       "Financial irregularities",
			Triage:      "high",
			Description: "Documents reference off-book payments to Atlas Holdings and Northbridge Consulting that lack standard procurement approvals. Triage: high because importance is high and evidence quality is medium.",
		},
		{
			ID:          2,
			Title:       "Internal communications",
			Triage:      "medium",
			Description: "Email correspondence between staff discussing the Atlas payments and proposing to move further conversations off-record. Triage: medium because importance is medium and evidence quality is medium.",
		},
		{
			ID:          3,
			Title:       "External correspondence",
			Triage:      "low",
			Description: "Formal disclosures to external compliance auditors stating no off-book arrangements exist. Triage: low because importance is low and evidence quality is medium.",
		},
	},
}

var mockTopicDetails = map[int]topicDetailResponse{
	1: {
		CaseID: mockCaseID,
		Topic: topicDetail{
			ID:            1,
			Title:         "Financial irregularities",
			Triage:        "high",
			Description:   "Documents reference off-book payments to Atlas Holdings and Northbridge Consulting that lack standard procurement approvals. Triage: high because importance is high and evidence quality is medium.",
			DocumentCount: 2,
			Heuristics: []topicHeuristic{
				{Name: "importance", Rating: "high", Description: "Importance: The topic describes off-book payments and missing procurement approvals. Evidence quality is medium."},
				{Name: "evidence_quality", Rating: "medium", Description: "Evidence quality used to gate final topic triage."},
				{Name: "sensitivity", Rating: "high", Description: "Highest sensitivity level in this topic is 4."},
				{Name: "claims", Rating: "high", Description: "5 factual claim(s) extracted in this topic."},
				{Name: "validation", Rating: "medium", Description: "Claim validation statuses: supported=3, unclear=2."},
				{Name: "document_types", Rating: "medium", Description: "Document types: invoice=1, memo=1."},
				{Name: "corroboration", Rating: "medium", Description: "The memo and invoice both reference Atlas payments, though their date ranges only partially overlap."},
				{Name: "shared_references", Rating: "medium", Description: "Atlas Holdings and payment amounts recur across the topic documents."},
				{Name: "coordinated_framing", Rating: "low", Description: "The documents use distinct administrative formats without shared inflammatory framing."},
				{Name: "shared_agenda", Rating: "low", Description: "The documents do not appear to advance a shared agenda beyond reporting payment details."},
			},
		},
	},
	2: {
		CaseID: mockCaseID,
		Topic: topicDetail{
			ID:            2,
			Title:         "Internal communications",
			Triage:        "medium",
			Description:   "Email correspondence between staff discussing the Atlas payments and proposing to move further conversations off-record. Triage: medium because importance is medium and evidence quality is medium.",
			DocumentCount: 1,
			Heuristics: []topicHeuristic{
				{Name: "importance", Rating: "medium", Description: "Importance: The topic describes internal handling of payment concerns. Evidence quality is medium."},
				{Name: "evidence_quality", Rating: "medium", Description: "Evidence quality used to gate final topic triage."},
				{Name: "sensitivity", Rating: "medium", Description: "Highest sensitivity level in this topic is 2."},
				{Name: "claims", Rating: "medium", Description: "2 factual claim(s) extracted in this topic."},
				{Name: "validation", Rating: "medium", Description: "Claim validation statuses: unclear=2."},
				{Name: "document_types", Rating: "medium", Description: "Document types: email=1."},
			},
		},
	},
	3: {
		CaseID: mockCaseID,
		Topic: topicDetail{
			ID:            3,
			Title:         "External correspondence",
			Triage:        "low",
			Description:   "Formal disclosures to external compliance auditors stating no off-book arrangements exist. Triage: low because importance is low and evidence quality is medium.",
			DocumentCount: 1,
			Heuristics: []topicHeuristic{
				{Name: "importance", Rating: "low", Description: "Importance: The topic is lower priority without corroborating claims in this mock case. Evidence quality is medium."},
				{Name: "evidence_quality", Rating: "medium", Description: "Evidence quality used to gate final topic triage."},
				{Name: "sensitivity", Rating: "low", Description: "Highest sensitivity level in this topic is 1."},
				{Name: "claims", Rating: "low", Description: "0 factual claim(s) extracted in this topic."},
				{Name: "document_types", Rating: "medium", Description: "Document types: letter=1."},
			},
		},
	},
}

var mockTopicDocuments = map[int]topicDocumentsResponse{
	1: {
		CaseID:  mockCaseID,
		TopicID: 1,
		Documents: []documentResponse{
			{
				ID:       1,
				Filename: "memo-2025-04-15.txt",
				Content: `INTERNAL MEMO

Date: 2025-04-15
From: J. Doe, Finance
To:   M. Smith, Procurement

Re: Q2 off-cycle vendor disbursements

Following our discussion last Thursday, I'm flagging three payments processed
outside the standard procurement workflow:

  2025-03-22  $147,500  Atlas Holdings          (no PO on file)
  2025-04-02  $ 89,200  Atlas Holdings          (no PO on file)
  2025-04-09  $212,000  Northbridge Consulting  (PO #84221 — terms unclear)

Per finance policy section 4.2 these require sign-off from two officers. I have
not been able to locate the second signature on any of the above. Could you
confirm who approved these on your side?

— J.`,
				Heuristics: []heuristic{
					{Name: "consistency", Signal: "positive", Rating: "high", Description: "Dates, dollar amounts, and counterparties are internally consistent and match the Atlas invoice in this topic."},
					{Name: "references", Signal: "positive", Rating: "high", Description: "Specific dates, dollar amounts, vendor names, and a PO number provide multiple checkable references."},
					{Name: "emotive_language", Signal: "negative", Rating: "low", Description: "Procedural tone; no inflammatory language or personal attacks."},
					{Name: "ideology", Signal: "negative", Rating: "low", Description: "Reads as a routine procedural escalation; no agenda or personal incentive evident."},
				},
			},
			{
				ID:       2,
				Filename: "atlas-invoice-q1-final.txt",
				Content: `Atlas Holdings — Invoice 2025-Q1-final
For services rendered Jan–Mar 2025

  Item                                       USD
  -----------------------------------------------
  Strategic advisory (retainer)           85,000
  Special project (off-book)              62,500
  Discretionary expenses                  15,000
  -----------------------------------------------
  TOTAL                                  162,500

Wire instructions provided separately.
This invoice is confidential and should not be shared with audit.`,
				Heuristics: []heuristic{
					{Name: "consistency", Signal: "positive", Rating: "medium", Description: "Total aligns with one line item in the memo, but the invoice predates the dates referenced there."},
					{Name: "references", Signal: "positive", Rating: "low", Description: "No invoice-number scheme that matches Atlas Holdings' publicly known billing format; no transaction or wire reference."},
					{Name: "emotive_language", Signal: "negative", Rating: "low", Description: "Tone is administrative; no emotional rhetoric."},
					{Name: "ideology", Signal: "negative", Rating: "low", Description: "No advocacy framing; the document presents itself as a routine billing artifact."},
				},
			},
		},
	},
	2: {
		CaseID:  mockCaseID,
		TopicID: 2,
		Documents: []documentResponse{
			{
				ID:       3,
				Filename: "email-alice-bob-2025-04-10.txt",
				Content: `From:    alice@example.com
To:      bob@example.com
Date:    Thu, 10 Apr 2025 14:22:00 +0000
Subject: re: re: Atlas thing

Bob —

I think we need to be careful here. J. from finance is asking about the Atlas
payments. I told her we'd circle back next week. Can you make sure the PO
records are tidy before then?

Also — can we move these conversations off email going forward? Use the
encrypted channel.

— A.`,
				Heuristics: []heuristic{
					{Name: "consistency", Signal: "positive", Rating: "high", Description: "Timing and named parties (J., Atlas) reinforce the financial-irregularity documents in this case."},
					{Name: "references", Signal: "positive", Rating: "low", Description: "Refers to an unspecified encrypted channel and unnamed PO records — neither externally verifiable."},
					{Name: "emotive_language", Signal: "negative", Rating: "low", Description: "Conversational but restrained; no insults or inflammatory language."},
					{Name: "ideology", Signal: "negative", Rating: "medium", Description: "Direct request to move discussion off email and 'tidy' PO records suggests an evasive incentive rather than neutral reporting."},
				},
			},
		},
	},
	3: {
		CaseID:  mockCaseID,
		TopicID: 3,
		Documents: []documentResponse{
			{
				ID:       4,
				Filename: "compliance-letter-2025-03-28.txt",
				Content: `March 28, 2025

Office of the Compliance Auditor
Re: Annual disclosure under Section 14B

To whom it may concern,

This letter confirms that all material related-party transactions for fiscal
year 2024 have been disclosed in our quarterly filings. We affirm that no
off-book arrangements exist between the company and any third party in which
an officer holds a beneficial interest.

Sincerely,
[Signed]
Office of the General Counsel`,
				Heuristics: []heuristic{
					{Name: "consistency", Signal: "positive", Rating: "low", Description: "Directly contradicts the memo and invoice in this case, which describe undisclosed and explicitly off-book payments."},
					{Name: "references", Signal: "positive", Rating: "high", Description: "Letterhead, addressee, and signing office match the organization's publicly listed General Counsel."},
					{Name: "emotive_language", Signal: "negative", Rating: "low", Description: "Formal regulatory register; no inflammatory language."},
					{Name: "ideology", Signal: "negative", Rating: "low", Description: "No advocacy framing or personal incentive evident; reads as a routine compliance disclosure."},
				},
			},
		},
	},
}

func requireKnownMockCase(w http.ResponseWriter, r *http.Request) bool {
	caseID, ok := parseID(w, r, "case_id", "case_not_found")
	if !ok {
		return false
	}
	if caseID != mockCaseID {
		writeError(w, http.StatusNotFound, "case_not_found", "no case with id "+strconv.Itoa(caseID))
		return false
	}
	return true
}

func mockCreateCaseHandler(w http.ResponseWriter, r *http.Request) {
	var req createCaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "request body is not valid JSON")
		return
	}
	if len(req.Documents) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "documents must contain at least one entry")
		return
	}
	for i, d := range req.Documents {
		if d.Content == "" {
			writeError(w, http.StatusBadRequest, "invalid_request", "documents["+strconv.Itoa(i)+"].content is required")
			return
		}
	}
	writeJSON(w, http.StatusCreated, createCaseResponse{CaseID: mockCaseID})
}

func mockGetCaseHandler(w http.ResponseWriter, r *http.Request) {
	if !requireKnownMockCase(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, mockCaseSummary)
}

func mockGetTopicHandler(w http.ResponseWriter, r *http.Request) {
	if !requireKnownMockCase(w, r) {
		return
	}
	topicID, ok := parseID(w, r, "topic_id", "topic_not_found")
	if !ok {
		return
	}
	resp, ok := mockTopicDetails[topicID]
	if !ok {
		writeError(w, http.StatusNotFound, "topic_not_found", "no topic with id "+strconv.Itoa(topicID)+" in case "+strconv.Itoa(mockCaseID))
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func mockGetTopicDocumentsHandler(w http.ResponseWriter, r *http.Request) {
	if !requireKnownMockCase(w, r) {
		return
	}
	topicID, ok := parseID(w, r, "topic_id", "topic_not_found")
	if !ok {
		return
	}
	resp, ok := mockTopicDocuments[topicID]
	if !ok {
		writeError(w, http.StatusNotFound, "topic_not_found", "no topic with id "+strconv.Itoa(topicID)+" in case "+strconv.Itoa(mockCaseID))
		return
	}
	writeJSON(w, http.StatusOK, resp)
}
