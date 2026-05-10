package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"
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

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func registerMockHandlers(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/cases", mockCreateCaseHandler)
	mux.HandleFunc("GET /api/v1/cases/{case_id}", mockGetCaseHandler)
	mux.HandleFunc("GET /api/v1/cases/{case_id}/topics/{topic_id}", mockGetTopicHandler)
	mux.HandleFunc("GET /api/v1/cases/{case_id}/topics/{topic_id}/documents", mockGetTopicDocumentsHandler)
}

func main() {
	nomock := flag.Bool("nomock", false, "use Maple-backed in-memory classification instead of hardcoded mock data")
	useClaude := flag.Bool("claude", false, "use Anthropic Claude (ANTHROPIC_API_KEY) as the classification backend instead of Maple; implies live (non-mock) classification")
	flag.Parse()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthcheck", healthHandler)

	switch {
	case *useClaude:
		apiKey := os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			log.Fatal("--claude requires ANTHROPIC_API_KEY to be set")
		}
		timeout := claudeTimeoutFromEnv()
		model := envOrDefault("CLAUDE_MODEL", defaultClaudeModel)
		baseURL := envOrDefault("CLAUDE_BASE_URL", defaultClaudeBaseURL)
		log.Printf("claude client: timeout=%s base_url=%s model=%s", timeout, baseURL, model)
		client := claudeClient{
			baseURL:   baseURL,
			apiKey:    apiKey,
			model:     model,
			maxTokens: claudeMaxTokensFromEnv(),
			httpClient: &http.Client{
				Timeout: timeout,
			},
		}
		classifier := mapleClassifier{
			model:  model,
			client: client,
		}
		registerMapleHandlers(mux, newMapleServer(classifier))
	case *nomock:
		apiKey := os.Getenv("MAPLE_API_KEY")
		if apiKey == "" {
			log.Fatal("--nomock requires MAPLE_API_KEY to be set")
		}
		timeout := mapleTimeoutFromEnv()
		log.Printf("maple client: timeout=%s base_url=%s", timeout, envOrDefault("MAPLE_BASE_URL", defaultMapleBaseURL))
		client := mapleClient{
			baseURL: envOrDefault("MAPLE_BASE_URL", defaultMapleBaseURL),
			apiKey:  apiKey,
			httpClient: &http.Client{
				Timeout: timeout,
			},
		}
		classifier := mapleClassifier{
			model:  envOrDefault("MAPLE_MODEL", defaultMapleModel),
			client: client,
		}
		registerMapleHandlers(mux, newMapleServer(classifier))
	default:
		registerMockHandlers(mux)
	}

	addr := ":" + port
	log.Printf("listening on %s (nomock=%v claude=%v)", addr, *nomock, *useClaude)
	if err := http.ListenAndServe(addr, withCORS(mux)); err != nil {
		log.Fatal(err)
	}
}

// mapleTimeoutFromEnv reads MAPLE_TIMEOUT_SECONDS and returns it as a
// time.Duration. Falls back to defaultMapleTimeout for unset, non-numeric, or
// non-positive values, logging the override failure so it's visible at
// startup rather than silently masking a typo.
func mapleTimeoutFromEnv() time.Duration {
	raw := os.Getenv("MAPLE_TIMEOUT_SECONDS")
	if raw == "" {
		return defaultMapleTimeout
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		log.Printf("MAPLE_TIMEOUT_SECONDS=%q is not a positive integer; using default %s", raw, defaultMapleTimeout)
		return defaultMapleTimeout
	}
	return time.Duration(seconds) * time.Second
}
