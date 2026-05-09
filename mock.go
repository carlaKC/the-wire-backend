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
			Description: "Documents reference off-book payments to Atlas Holdings and Northbridge Consulting that lack standard procurement approvals.",
		},
		{
			ID:          2,
			Title:       "Internal communications",
			Triage:      "medium",
			Description: "Email correspondence between staff discussing the Atlas payments and proposing to move further conversations off-record.",
		},
		{
			ID:          3,
			Title:       "External correspondence",
			Triage:      "low",
			Description: "Formal disclosures to external compliance auditors stating no off-book arrangements exist.",
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
			Description:   "Documents reference off-book payments to Atlas Holdings and Northbridge Consulting that lack standard procurement approvals.",
			DocumentCount: 2,
			Heuristics: []heuristic{
				{Name: "consistency", Rating: "high", Description: "Dates, amounts, and counterparty names corroborate across the memo and the invoice."},
				{Name: "references", Rating: "medium", Description: "Atlas Holdings and Northbridge Consulting both appear in public business registries; the cited PO number could not be verified."},
				{Name: "red_flags", Rating: "high", Description: "One document explicitly instructs the recipient not to share with audit, and another lists 'off-book' as a line item."},
				{Name: "language_signals", Rating: "medium", Description: "Tone and terminology are consistent with internal finance correspondence at this organization."},
			},
		},
	},
	2: {
		CaseID: mockCaseID,
		Topic: topicDetail{
			ID:            2,
			Title:         "Internal communications",
			Triage:        "medium",
			Description:   "Email correspondence between staff discussing the Atlas payments and proposing to move further conversations off-record.",
			DocumentCount: 1,
			Heuristics: []heuristic{
				{Name: "consistency", Rating: "high", Description: "Reinforces the timeline and named parties found in the financial documents."},
				{Name: "references", Rating: "low", Description: "The thread points readers to a private encrypted channel; underlying conversation is not externally verifiable."},
				{Name: "red_flags", Rating: "medium", Description: "Explicit suggestion to move the discussion off email is a documented evasion pattern."},
				{Name: "language_signals", Rating: "high", Description: "Idiom, signoffs, and email signature match other messages from the same author."},
			},
		},
	},
	3: {
		CaseID: mockCaseID,
		Topic: topicDetail{
			ID:            3,
			Title:         "External correspondence",
			Triage:        "low",
			Description:   "Formal disclosures to external compliance auditors stating no off-book arrangements exist.",
			DocumentCount: 1,
			Heuristics: []heuristic{
				{Name: "consistency", Rating: "low", Description: "Directly contradicts the financial-irregularity documents in this same case, which describe undisclosed payments."},
				{Name: "references", Rating: "high", Description: "Letterhead and signing officer match the organization's publicly listed General Counsel."},
				{Name: "red_flags", Rating: "low", Description: "No anomalies in formatting, metadata, or formal language."},
				{Name: "language_signals", Rating: "high", Description: "Formal register consistent with regulatory correspondence."},
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
					{Name: "consistency", Rating: "high", Description: "Dates and counterparties match the Atlas invoice in this topic."},
					{Name: "references", Rating: "medium", Description: "Atlas Holdings and Northbridge Consulting are verifiable in public registries; PO #84221 is not."},
					{Name: "red_flags", Rating: "low", Description: "Format, header, and tone match other internal memos from this organization."},
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
					{Name: "consistency", Rating: "medium", Description: "Total aligns with one line item in the memo, but issued before the dates referenced there."},
					{Name: "references", Rating: "low", Description: "No invoice number scheme matches Atlas Holdings' publicly known billing format."},
					{Name: "red_flags", Rating: "high", Description: "Line item literally reads 'off-book' and the document instructs the recipient not to share with audit — strong indicator of intent to conceal."},
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
					{Name: "consistency", Rating: "high", Description: "Timing and named parties (J., Atlas) reinforce the financial-irregularity documents."},
					{Name: "references", Rating: "low", Description: "Refers to an unspecified encrypted channel and unnamed PO records — neither externally verifiable."},
					{Name: "red_flags", Rating: "medium", Description: "Direct request to move discussion off email plus instruction to 'tidy' PO records."},
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
					{Name: "consistency", Rating: "low", Description: "Directly contradicts the memo and invoice in this case, which describe undisclosed and explicitly off-book payments."},
					{Name: "references", Rating: "high", Description: "Letterhead, addressee, and signing office match the organization's publicly listed General Counsel."},
					{Name: "red_flags", Rating: "low", Description: "Document itself shows no anomalies; the concern is corroboration with other case documents, not the document's own integrity."},
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
