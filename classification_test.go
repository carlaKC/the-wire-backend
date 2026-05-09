package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestMapleClientChatCompletion(t *testing.T) {
	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.String() != "http://maple.test/v1/chat/completions" {
				t.Errorf("url = %q, want http://maple.test/v1/chat/completions", r.URL.String())
			}
			if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
				t.Errorf("Authorization = %q, want Bearer test-key", got)
			}

			var req chatRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if req.Model != "test-model" {
				t.Errorf("model = %q, want test-model", req.Model)
			}
			if len(req.Messages) != 1 || req.Messages[0].Content != "summarize this" {
				t.Errorf("messages = %#v", req.Messages)
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Body:       io.NopCloser(strings.NewReader(buildChatResponse("done"))),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Request:    r,
			}, nil
		}),
	}

	client := mapleClient{
		baseURL:    "http://maple.test",
		apiKey:     "test-key",
		httpClient: httpClient,
	}
	got, err := client.ChatCompletion(context.Background(), chatRequest{
		Model: "test-model",
		Messages: []chatMessage{
			{Role: "user", Content: "summarize this"},
		},
	})
	if err != nil {
		t.Fatalf("ChatCompletion returned error: %v", err)
	}
	if got != "done" {
		t.Errorf("content = %q, want done", got)
	}
}

func TestMapleClientChatCompletionRequiresAPIKey(t *testing.T) {
	client := mapleClient{
		baseURL: "http://maple.test",
		httpClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				t.Fatal("transport should not be called without an API key")
				return nil, nil
			}),
		},
	}

	_, err := client.ChatCompletion(context.Background(), chatRequest{
		Model: "test-model",
		Messages: []chatMessage{
			{Role: "user", Content: "summarize this"},
		},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "MAPLE_API_KEY is required" {
		t.Errorf("error = %q, want MAPLE_API_KEY is required", err.Error())
	}
}

func TestMapleClientChatCompletionHandlesMapleErrors(t *testing.T) {
	client := mapleClient{
		baseURL: "http://maple.test",
		apiKey:  "test-key",
		httpClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Body: io.NopCloser(strings.NewReader(`{
						"error": { "type": "bad_request", "message": "invalid prompt" }
					}`)),
					Request: r,
				}, nil
			}),
		},
	}

	_, err := client.ChatCompletion(context.Background(), chatRequest{
		Model: "test-model",
		Messages: []chatMessage{
			{Role: "user", Content: "summarize this"},
		},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "maple error bad_request: invalid prompt" {
		t.Errorf("error = %q, want maple error bad_request: invalid prompt", err.Error())
	}
}

func buildChatResponse(content string) string {
	respBody, err := json.Marshal(chatResponse{
		Choices: []struct {
			Message chatMessage `json:"message"`
		}{
			{Message: chatMessage{Role: "assistant", Content: content}},
		},
	})
	if err != nil {
		panic(err)
	}
	return string(respBody)
}

type recordingChatCompleter struct {
	requests []chatRequest
	contents []string
}

func (c *recordingChatCompleter) ChatCompletion(_ context.Context, req chatRequest) (string, error) {
	c.requests = append(c.requests, req)
	content := c.contents[0]
	c.contents = c.contents[1:]
	return content, nil
}

func TestMapleClassifierUsesInjectedChatCompleter(t *testing.T) {
	completer := &recordingChatCompleter{
		contents: []string{`{"documents":[{"id":"doc-1","topic":{"title":"Ops","category":"ops","confidence":0.9},"document_type":{"category":"memo","confidence":0.8},"sensitivity":{"level":2,"label":"medium","confidence":0.7},"rationale":"ops memo","claims":[]}]}`},
	}
	classifier := mapleClassifier{
		model:  "classification-model",
		client: completer,
	}

	report, err := classifier.Classify(context.Background(), []classifiedInput{
		{ID: "doc-1", Filename: "memo.txt", Content: "memo body"},
	}, []categoryCandidate{{ID: 7, Title: "Existing", Description: "Existing category"}})
	if err != nil {
		t.Fatalf("Classify returned error: %v", err)
	}
	if len(report.Documents) != 1 || report.Documents[0].ID != "doc-1" {
		t.Fatalf("documents = %#v", report.Documents)
	}
	if len(completer.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(completer.requests))
	}
	if completer.requests[0].Model != "classification-model" {
		t.Errorf("model = %q, want classification-model", completer.requests[0].Model)
	}
	if completer.requests[0].ResponseFormat.Type != "json_object" {
		t.Errorf("response format = %q, want json_object", completer.requests[0].ResponseFormat.Type)
	}
}
