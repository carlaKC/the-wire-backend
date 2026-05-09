package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

type heuristic struct {
	Name        string `json:"name"`
	Rating      string `json:"rating"`
	Description string `json:"description"`
}

type storedDoc struct {
	ID         int
	Filename   string
	Content    string
	Heuristics []heuristic
}

type storedCategory struct {
	ID          int
	Title       string
	Triage      string
	Description string
	Heuristics  []heuristic
	Documents   []storedDoc
}

type storedCase struct {
	ID         int
	CreatedAt  time.Time
	DocCount   int
	Categories []storedCategory
}

type store struct {
	mu     sync.Mutex
	nextID int
	cases  map[int]*storedCase
}

func newStore() *store {
	return &store{cases: map[int]*storedCase{}}
}

func (s *store) create(c *storedCase) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	c.ID = s.nextID
	s.cases[c.ID] = c
}

func (s *store) get(id int) (*storedCase, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.cases[id]
	return c, ok
}

// Request/response types

type docInput struct {
	Filename string `json:"filename"`
	Content  string `json:"content"`
}

type createCaseRequest struct {
	Documents []docInput `json:"documents"`
}

type createCaseResponse struct {
	CaseID int `json:"case_id"`
}

type categorySummary struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Triage      string `json:"triage"`
	Description string `json:"description"`
}

type caseSummaryResponse struct {
	CaseID        int               `json:"case_id"`
	CreatedAt     string            `json:"created_at"`
	DocumentCount int               `json:"document_count"`
	Categories    []categorySummary `json:"categories"`
}

type categoryDetail struct {
	ID            int         `json:"id"`
	Title         string      `json:"title"`
	Triage        string      `json:"triage"`
	Description   string      `json:"description"`
	DocumentCount int         `json:"document_count"`
	Heuristics    []heuristic `json:"heuristics"`
}

type categoryDetailResponse struct {
	CaseID   int            `json:"case_id"`
	Category categoryDetail `json:"category"`
}

type documentResponse struct {
	ID         int         `json:"id"`
	Filename   string      `json:"filename"`
	Content    string      `json:"content"`
	Heuristics []heuristic `json:"heuristics"`
}

type categoryDocumentsResponse struct {
	CaseID     int                `json:"case_id"`
	CategoryID int                `json:"category_id"`
	Documents  []documentResponse `json:"documents"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorResponse struct {
	Error errorBody `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, errorResponse{Error: errorBody{Code: code, Message: msg}})
}

// Mock data

var mockCategoryTemplates = []storedCategory{
	{
		Title:       "Financial irregularities",
		Triage:      "high",
		Description: "Documents reference off-book payments, unusual vendor relationships, or accounting practices that warrant investigation.",
		Heuristics: []heuristic{
			{Name: "consistency", Rating: "high", Description: "Dates, amounts, and counterparties corroborate across the documents in this category."},
			{Name: "references", Rating: "medium", Description: "Some external references (invoice IDs, vendor names) are verifiable in public records."},
			{Name: "red_flags", Rating: "low", Description: "Metadata, formatting, and language are consistent with templates from the originating organization."},
			{Name: "language_signals", Rating: "medium", Description: "Tone and terminology match what you'd expect in internal financial communications."},
		},
	},
	{
		Title:       "Internal communications",
		Triage:      "medium",
		Description: "Email threads, memos, and chat logs discussing internal decisions, timelines, and disputes.",
		Heuristics: []heuristic{
			{Name: "consistency", Rating: "medium", Description: "Most facts are corroborated, though one thread contradicts another on a key date."},
			{Name: "references", Rating: "low", Description: "References are mostly internal; limited ability to verify externally."},
			{Name: "red_flags", Rating: "medium", Description: "One forwarded message is missing original Message-ID headers."},
			{Name: "language_signals", Rating: "high", Description: "Idiom and signoff patterns match other documents from the same author."},
		},
	},
	{
		Title:       "External correspondence",
		Triage:      "low",
		Description: "Letters, contracts, and emails exchanged with external parties such as vendors, regulators, or partners.",
		Heuristics: []heuristic{
			{Name: "consistency", Rating: "high", Description: "External party names, addresses, and dates are internally consistent."},
			{Name: "references", Rating: "high", Description: "Most named entities are verifiable via public registries."},
			{Name: "red_flags", Rating: "low", Description: "No obvious indicators of fabrication or tampering."},
			{Name: "language_signals", Rating: "medium", Description: "Formal register consistent with business correspondence."},
		},
	},
}

var mockDocHeuristicSets = [][]heuristic{
	{
		{Name: "consistency", Rating: "high", Description: "Aligns closely with the other documents in this category on dates and named parties."},
		{Name: "references", Rating: "medium", Description: "Most named entities are verifiable in public records; one could not be confirmed."},
		{Name: "red_flags", Rating: "low", Description: "Header and footer styling match other documents from the originating organization."},
	},
	{
		{Name: "consistency", Rating: "high", Description: "Reinforces the timeline and amounts mentioned in sibling documents."},
		{Name: "references", Rating: "low", Description: "References private channels and unnamed parties; difficult to verify externally."},
		{Name: "red_flags", Rating: "medium", Description: "Appears to be a forwarded message missing the original Message-ID header."},
	},
	{
		{Name: "consistency", Rating: "medium", Description: "Largely consistent with other documents, but one detail conflicts with another source."},
		{Name: "references", Rating: "high", Description: "All named external parties match entries in public registries."},
		{Name: "red_flags", Rating: "low", Description: "No anomalies in formatting, metadata, or language use."},
	},
}

func buildMockCase(now time.Time, docs []docInput) *storedCase {
	categories := make([]storedCategory, 0, len(mockCategoryTemplates))
	for i, tpl := range mockCategoryTemplates {
		c := tpl
		c.ID = i + 1
		c.Documents = nil
		categories = append(categories, c)
	}

	docID := 0
	for i, d := range docs {
		docID++
		filename := d.Filename
		if filename == "" {
			filename = "document-" + strconv.Itoa(docID) + ".txt"
		}
		sd := storedDoc{
			ID:         docID,
			Filename:   filename,
			Content:    d.Content,
			Heuristics: mockDocHeuristicSets[i%len(mockDocHeuristicSets)],
		}
		ci := i % len(categories)
		categories[ci].Documents = append(categories[ci].Documents, sd)
	}

	pruned := categories[:0]
	for _, c := range categories {
		if len(c.Documents) > 0 {
			pruned = append(pruned, c)
		}
	}

	return &storedCase{
		CreatedAt:  now,
		DocCount:   len(docs),
		Categories: pruned,
	}
}

// Handlers

func createCaseHandler(s *store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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

		c := buildMockCase(time.Now().UTC(), req.Documents)
		s.create(c)
		writeJSON(w, http.StatusCreated, createCaseResponse{CaseID: c.ID})
	}
}

func getCaseHandler(s *store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		caseID, ok := parseID(w, r, "case_id", "case_not_found")
		if !ok {
			return
		}
		c, ok := s.get(caseID)
		if !ok {
			writeError(w, http.StatusNotFound, "case_not_found", "no case with id "+strconv.Itoa(caseID))
			return
		}

		summaries := make([]categorySummary, 0, len(c.Categories))
		for _, cat := range c.Categories {
			summaries = append(summaries, categorySummary{
				ID:          cat.ID,
				Title:       cat.Title,
				Triage:      cat.Triage,
				Description: cat.Description,
			})
		}
		writeJSON(w, http.StatusOK, caseSummaryResponse{
			CaseID:        c.ID,
			CreatedAt:     c.CreatedAt.Format(time.RFC3339),
			DocumentCount: c.DocCount,
			Categories:    summaries,
		})
	}
}

func getCategoryHandler(s *store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, cat, ok := lookupCategory(w, r, s)
		if !ok {
			return
		}
		writeJSON(w, http.StatusOK, categoryDetailResponse{
			CaseID: c.ID,
			Category: categoryDetail{
				ID:            cat.ID,
				Title:         cat.Title,
				Triage:        cat.Triage,
				Description:   cat.Description,
				DocumentCount: len(cat.Documents),
				Heuristics:    cat.Heuristics,
			},
		})
	}
}

func getCategoryDocumentsHandler(s *store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, cat, ok := lookupCategory(w, r, s)
		if !ok {
			return
		}
		docs := make([]documentResponse, 0, len(cat.Documents))
		for _, d := range cat.Documents {
			docs = append(docs, documentResponse{
				ID:         d.ID,
				Filename:   d.Filename,
				Content:    d.Content,
				Heuristics: d.Heuristics,
			})
		}
		writeJSON(w, http.StatusOK, categoryDocumentsResponse{
			CaseID:     c.ID,
			CategoryID: cat.ID,
			Documents:  docs,
		})
	}
}

func parseID(w http.ResponseWriter, r *http.Request, name, notFoundCode string) (int, bool) {
	raw := r.PathValue(name)
	id, err := strconv.Atoi(raw)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, notFoundCode, "no "+name+" with id "+raw)
		return 0, false
	}
	return id, true
}

func lookupCategory(w http.ResponseWriter, r *http.Request, s *store) (*storedCase, *storedCategory, bool) {
	caseID, ok := parseID(w, r, "case_id", "case_not_found")
	if !ok {
		return nil, nil, false
	}
	categoryID, ok := parseID(w, r, "category_id", "category_not_found")
	if !ok {
		return nil, nil, false
	}
	c, ok := s.get(caseID)
	if !ok {
		writeError(w, http.StatusNotFound, "case_not_found", "no case with id "+strconv.Itoa(caseID))
		return nil, nil, false
	}
	for i := range c.Categories {
		if c.Categories[i].ID == categoryID {
			return c, &c.Categories[i], true
		}
	}
	writeError(w, http.StatusNotFound, "category_not_found", "no category with id "+strconv.Itoa(categoryID)+" in case "+strconv.Itoa(caseID))
	return nil, nil, false
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	s := newStore()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/cases", createCaseHandler(s))
	mux.HandleFunc("GET /api/v1/cases/{case_id}", getCaseHandler(s))
	mux.HandleFunc("GET /api/v1/cases/{case_id}/categories/{category_id}", getCategoryHandler(s))
	mux.HandleFunc("GET /api/v1/cases/{case_id}/categories/{category_id}/documents", getCategoryDocumentsHandler(s))

	addr := ":" + port
	log.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, withCORS(mux)); err != nil {
		log.Fatal(err)
	}
}
