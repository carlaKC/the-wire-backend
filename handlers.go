package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
)

type server struct {
	store *store
	ctx   context.Context
}

func (s *server) createCaseHandler(w http.ResponseWriter, r *http.Request) {
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

	k := s.store.createCase(req.Documents)
	processCase(s.ctx, s.store, k.ID)
	writeJSON(w, http.StatusCreated, createCaseResponse{CaseID: k.ID})
}

func (s *server) getCaseHandler(w http.ResponseWriter, r *http.Request) {
	caseID, ok := parseID(w, r, "case_id", "case_not_found")
	if !ok {
		return
	}
	resp, err := s.store.caseSummary(caseID)
	if errors.Is(err, errCaseNotFound) {
		writeError(w, http.StatusNotFound, "case_not_found", "no case with id "+strconv.Itoa(caseID))
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *server) getCategoryHandler(w http.ResponseWriter, r *http.Request) {
	caseID, ok := parseID(w, r, "case_id", "case_not_found")
	if !ok {
		return
	}
	categoryID, ok := parseID(w, r, "category_id", "category_not_found")
	if !ok {
		return
	}
	resp, err := s.store.categoryDetail(caseID, categoryID)
	switch {
	case errors.Is(err, errCaseNotFound):
		writeError(w, http.StatusNotFound, "case_not_found", "no case with id "+strconv.Itoa(caseID))
		return
	case errors.Is(err, errCategoryNotFound):
		writeError(w, http.StatusNotFound, "category_not_found", "no category with id "+strconv.Itoa(categoryID)+" in case "+strconv.Itoa(caseID))
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *server) getCategoryDocumentsHandler(w http.ResponseWriter, r *http.Request) {
	caseID, ok := parseID(w, r, "case_id", "case_not_found")
	if !ok {
		return
	}
	categoryID, ok := parseID(w, r, "category_id", "category_not_found")
	if !ok {
		return
	}
	resp, err := s.store.categoryDocuments(caseID, categoryID)
	switch {
	case errors.Is(err, errCaseNotFound):
		writeError(w, http.StatusNotFound, "case_not_found", "no case with id "+strconv.Itoa(caseID))
		return
	case errors.Is(err, errCategoryNotFound):
		writeError(w, http.StatusNotFound, "category_not_found", "no category with id "+strconv.Itoa(categoryID)+" in case "+strconv.Itoa(caseID))
		return
	}
	writeJSON(w, http.StatusOK, resp)
}
