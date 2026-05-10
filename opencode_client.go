package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	defaultOpencodeBin        = "opencode"
	defaultOpencodeOutputTail = 4096
)

type opencodeCommandRunner func(ctx context.Context, bin string, args ...string) (stdout, stderr string, err error)

// opencodeClient implements mapleChatCompleter by shelling out to the
// non-interactive opencode CLI. It follows the same contract as the direct
// Maple and Claude adapters: the classifier gives it a chat request, and it
// returns the assistant's text output.
type opencodeClient struct {
	bin     string
	model   string
	timeout time.Duration
	runner  opencodeCommandRunner
}

func (c opencodeClient) ChatCompletion(ctx context.Context, reqBody chatRequest) (string, error) {
	model := c.model
	if model == "" {
		model = reqBody.Model
	}
	if model == "" {
		return "", errors.New("OPENCODE_MODEL is required")
	}

	bin := c.bin
	if bin == "" {
		bin = defaultOpencodeBin
	}
	runner := c.runner
	if runner == nil {
		runner = runOpencodeCommand
	}
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}

	prompt := renderOpencodePrompt(reqBody)
	stdout, stderr, err := runner(ctx, bin, "run", "-m", model, prompt)
	if err != nil {
		return "", fmt.Errorf("opencode run failed: %w%s", err, commandOutputSuffix(stderr, stdout))
	}
	content := strings.TrimSpace(stdout)
	if content == "" {
		return "", fmt.Errorf("opencode run returned empty output%s", commandOutputSuffix(stderr, stdout))
	}
	return content, nil
}

func renderOpencodePrompt(req chatRequest) string {
	var b strings.Builder
	if req.ResponseFormat.Type == "json_object" {
		b.WriteString("Return only valid JSON. Do not wrap it in Markdown fences or add explanatory prose.\n\n")
	}
	for _, msg := range req.Messages {
		role := strings.TrimSpace(msg.Role)
		if role == "" {
			role = "message"
		}
		b.WriteString(strings.ToUpper(role))
		b.WriteString(":\n")
		b.WriteString(strings.TrimSpace(msg.Content))
		b.WriteString("\n\n")
	}
	return strings.TrimSpace(b.String())
}

func runOpencodeCommand(ctx context.Context, bin string, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return stdout.String(), stderr.String(), ctxErr
	}
	return stdout.String(), stderr.String(), err
}

func commandOutputSuffix(stderr, stdout string) string {
	stderr = strings.TrimSpace(stderr)
	stdout = strings.TrimSpace(stdout)
	if stderr == "" && stdout == "" {
		return ""
	}
	var b strings.Builder
	if stderr != "" {
		b.WriteString(": stderr=")
		b.WriteString(quoteOutputTail(stderr))
	}
	if stdout != "" {
		if b.Len() == 0 {
			b.WriteString(": ")
		} else {
			b.WriteString(" ")
		}
		b.WriteString("stdout=")
		b.WriteString(quoteOutputTail(stdout))
	}
	return b.String()
}

func quoteOutputTail(s string) string {
	return fmt.Sprintf("%q", tailString(s, defaultOpencodeOutputTail))
}

func tailString(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return "..." + s[len(s)-max:]
}

// opencodeTimeoutFromEnv mirrors the live model timeout helpers. Classification
// can take minutes, so invalid overrides fall back to the Maple/Claude default.
func opencodeTimeoutFromEnv() time.Duration {
	raw := envOrDefault("OPENCODE_TIMEOUT_SECONDS", "")
	if raw == "" {
		return defaultMapleTimeout
	}
	var n int
	if _, err := fmt.Sscanf(raw, "%d", &n); err != nil || n <= 0 {
		return defaultMapleTimeout
	}
	return time.Duration(n) * time.Second
}
