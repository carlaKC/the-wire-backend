package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"
)

const (
	defaultDocDelay        = 2 * time.Second
	defaultCatDelay        = 3 * time.Second
	defaultDocConcurrency  = 8
	defaultCaseConcurrency = 4
)

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, errorResponse{Error: errorBody{Code: code, Message: msg}})
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

func healthHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{Time: time.Now().UTC().Format(time.RFC3339)})
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

func registerMockHandlers(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/cases", mockCreateCaseHandler)
	mux.HandleFunc("GET /api/v1/cases/{case_id}", mockGetCaseHandler)
	mux.HandleFunc("GET /api/v1/cases/{case_id}/categories/{category_id}", mockGetCategoryHandler)
	mux.HandleFunc("GET /api/v1/cases/{case_id}/categories/{category_id}/documents", mockGetCategoryDocumentsHandler)
}

func registerNomockHandlers(mux *http.ServeMux, srv *server) {
	mux.HandleFunc("POST /api/v1/cases", srv.createCaseHandler)
	mux.HandleFunc("GET /api/v1/cases/{case_id}", srv.getCaseHandler)
	mux.HandleFunc("GET /api/v1/cases/{case_id}/categories/{category_id}", srv.getCategoryHandler)
	mux.HandleFunc("GET /api/v1/cases/{case_id}/categories/{category_id}/documents", srv.getCategoryDocumentsHandler)
}

func main() {
	nomock := flag.Bool("nomock", false, "use real in-memory implementation instead of hardcoded mock data")
	flag.Parse()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthcheck", healthHandler)

	if *nomock {
		s := newStore(defaultDocConcurrency, defaultCaseConcurrency, defaultDocDelay, defaultCatDelay)
		srv := &server{store: s, ctx: context.Background()}
		registerNomockHandlers(mux, srv)
	} else {
		registerMockHandlers(mux)
	}

	addr := ":" + port
	log.Printf("listening on %s (nomock=%v)", addr, *nomock)
	if err := http.ListenAndServe(addr, withCORS(mux)); err != nil {
		log.Fatal(err)
	}
}
