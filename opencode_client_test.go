package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestOpencodeClientChatCompletionRunsOpencode(t *testing.T) {
	var gotBin string
	var gotArgs []string
	client := opencodeClient{
		bin:   "oc",
		model: "openrouter/test-model",
		runner: func(_ context.Context, bin string, args ...string) (string, string, error) {
			gotBin = bin
			gotArgs = append([]string(nil), args...)
			return "  {\"ok\":true}\n", "", nil
		},
	}

	got, err := client.ChatCompletion(context.Background(), chatRequest{
		Model:          "ignored-model",
		ResponseFormat: responseFormat{Type: "json_object"},
		Messages: []chatMessage{
			{Role: "system", Content: "Classify documents."},
			{Role: "user", Content: "Document body"},
		},
	})
	if err != nil {
		t.Fatalf("ChatCompletion returned error: %v", err)
	}
	if got != `{"ok":true}` {
		t.Errorf("content = %q, want trimmed JSON", got)
	}
	if gotBin != "oc" {
		t.Errorf("bin = %q, want oc", gotBin)
	}
	if len(gotArgs) != 4 {
		t.Fatalf("args = %#v, want 4 args", gotArgs)
	}
	if gotArgs[0] != "run" || gotArgs[1] != "-m" || gotArgs[2] != "openrouter/test-model" {
		t.Fatalf("args prefix = %#v, want opencode run -m openrouter/test-model", gotArgs[:3])
	}
	prompt := gotArgs[3]
	for _, want := range []string{
		"Return only valid JSON",
		"SYSTEM:\nClassify documents.",
		"USER:\nDocument body",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestOpencodeClientUsesRequestModelWhenClientModelUnset(t *testing.T) {
	var gotModel string
	client := opencodeClient{
		runner: func(_ context.Context, _ string, args ...string) (string, string, error) {
			gotModel = args[2]
			return "done", "", nil
		},
	}

	_, err := client.ChatCompletion(context.Background(), chatRequest{
		Model:    "request-model",
		Messages: []chatMessage{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion returned error: %v", err)
	}
	if gotModel != "request-model" {
		t.Errorf("model = %q, want request-model", gotModel)
	}
}

func TestOpencodeClientRequiresModel(t *testing.T) {
	client := opencodeClient{
		runner: func(_ context.Context, _ string, _ ...string) (string, string, error) {
			t.Fatal("runner should not be called without a model")
			return "", "", nil
		},
	}

	_, err := client.ChatCompletion(context.Background(), chatRequest{
		Messages: []chatMessage{{Role: "user", Content: "hello"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "OPENCODE_MODEL is required" {
		t.Errorf("error = %q, want OPENCODE_MODEL is required", err.Error())
	}
}

func TestOpencodeClientIncludesBoundedCommandOutputOnFailure(t *testing.T) {
	longStderr := strings.Repeat("x", defaultOpencodeOutputTail+10)
	client := opencodeClient{
		model: "model",
		runner: func(_ context.Context, _ string, _ ...string) (string, string, error) {
			return "partial", longStderr, errors.New("exit status 1")
		},
	}

	_, err := client.ChatCompletion(context.Background(), chatRequest{
		Messages: []chatMessage{{Role: "user", Content: "hello"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "opencode run failed: exit status 1") {
		t.Errorf("error missing command failure: %s", msg)
	}
	if !strings.Contains(msg, "stderr=\"...") {
		t.Errorf("error missing bounded stderr tail: %s", msg)
	}
	if !strings.Contains(msg, "stdout=\"partial\"") {
		t.Errorf("error missing stdout: %s", msg)
	}
	if strings.Contains(msg, longStderr) {
		t.Errorf("error includes unbounded stderr")
	}
}

func TestOpencodeClientTimeoutCancelsRunner(t *testing.T) {
	client := opencodeClient{
		model:   "model",
		timeout: 10 * time.Millisecond,
		runner: func(ctx context.Context, _ string, _ ...string) (string, string, error) {
			<-ctx.Done()
			return "", "", ctx.Err()
		},
	}

	_, err := client.ChatCompletion(context.Background(), chatRequest{
		Messages: []chatMessage{{Role: "user", Content: "hello"}},
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context deadline exceeded", err)
	}
}

func TestOpencodeTimeoutFromEnv(t *testing.T) {
	t.Setenv("OPENCODE_TIMEOUT_SECONDS", "7")
	if got := opencodeTimeoutFromEnv(); got != 7*time.Second {
		t.Errorf("timeout = %s, want 7s", got)
	}

	t.Setenv("OPENCODE_TIMEOUT_SECONDS", "bad")
	if got := opencodeTimeoutFromEnv(); got != defaultMapleTimeout {
		t.Errorf("timeout = %s, want %s", got, defaultMapleTimeout)
	}
}
