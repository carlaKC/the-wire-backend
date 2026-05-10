// promptcorpus runs the production document-heuristics prompt against a
// hand-curated corpus of documents, compares each of the four returned
// heuristic ratings against an answer key, and — when at least one rating
// disagrees — asks the model to suggest concrete prompt edits, applies
// the proposed unified diff to a temp copy of the prompts directory,
// reruns the corpus against the patched prompt, and reports whether the
// total correctness score went up.
//
// The diff is only written to disk if the second pass strictly increased
// the matched-heuristic count across the corpus. Otherwise the proposed
// change is discarded and the verdict line records the regression.
//
// It is a separate binary from the HTTP server but uses the same backend
// flags and environment variables (ANTHROPIC_API_KEY, MAPLE_API_KEY,
// OPENCODE_MODEL, etc.) so it points at whichever model the developer is
// already configured to call.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const (
	defaultOpencodeBin   = "opencode"
	defaultClaudeModel   = "claude-sonnet-4-6"
	defaultClaudeBaseURL = "https://api.anthropic.com"
	defaultClaudeMaxTok  = 16384
	defaultMapleModel    = "deepseek-v4-pro"
	defaultMapleBaseURL  = "http://127.0.0.1:8081"
	defaultClientTimeout = 5 * time.Minute
)

func main() {
	corpusDir := flag.String("corpus", "test-data/batch1/prompt-corpus.json", "path to the corpus JSON file (an array of answer-key entries); document paths are resolved relative to its directory. May also be a directory containing prompt-corpus.json.")
	promptDir := flag.String("prompts", "prompts", "directory holding the production prompt files (document_heuristics_system.txt, document_heuristics_user.txt)")
	outputPath := flag.String("o", "", "summary-table output file (default stdout)")
	diffPath := flag.String("diff", "document_heuristics_system.diff", "file to write the model-proposed unified diff to when at least one heuristic mismatch is observed; named to match prompts/document_heuristics_system.txt, the file the diff applies to")
	rawOutput := flag.String("raw", "", "if set, write each model's raw heuristics JSON to this directory for inspection")
	useClaude := flag.Bool("claude", false, "use Anthropic Claude as the LLM backend (ANTHROPIC_API_KEY)")
	useOpencode := flag.Bool("opencode", false, "use opencode CLI as the LLM backend (OPENCODE_MODEL)")
	flag.Parse()

	backend, err := buildBackend(*useClaude, *useOpencode)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("backend: %s", backend.label())

	sysPrompt, userTpl, err := loadProductionPrompts(*promptDir)
	if err != nil {
		log.Fatalf("load prompts: %v", err)
	}

	docs, err := loadCorpus(*corpusDir)
	if err != nil {
		log.Fatalf("load corpus: %v", err)
	}
	if len(docs) == 0 {
		log.Fatalf("no answer keys found in %s", *corpusDir)
	}
	log.Printf("loaded %d documents", len(docs))

	if *rawOutput != "" {
		if err := os.MkdirAll(*rawOutput, 0o755); err != nil {
			log.Fatalf("create raw output dir: %v", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	baseline := runCorpus(ctx, backend, sysPrompt, userTpl, docs, *rawOutput, "baseline")
	baselineMatched, total := totalScore(baseline)

	report := reportInput{Baseline: baseline}
	report.Verdict, report.Improved = improveAndCompare(
		ctx, backend, *promptDir, sysPrompt, userTpl, docs,
		baseline, baselineMatched, total, *diffPath,
	)

	rendered := renderReport(report)

	out := os.Stdout
	if *outputPath != "" {
		f, err := os.Create(*outputPath)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		out = f
	}
	if _, err := out.Write([]byte(rendered)); err != nil {
		log.Fatal(err)
	}
	if *outputPath != "" {
		log.Printf("wrote summary table to %s", *outputPath)
	}
}

// runCorpus evaluates every doc once, optionally dumping each model's
// raw heuristics JSON for inspection. The label is interpolated into log
// lines so the baseline and improved passes can be told apart.
func runCorpus(ctx context.Context, backend chatClient, sysPrompt, userTpl string, docs []corpusDoc, rawDir, label string) []evaluationResult {
	results := make([]evaluationResult, 0, len(docs))
	for _, d := range docs {
		log.Printf("[%s] evaluating %s (%s)", label, d.ID, d.DocPath)
		r := evaluateDocument(ctx, backend, sysPrompt, userTpl, d)
		if rawDir != "" && r.RawModelOutput != "" {
			path := filepath.Join(rawDir, fmt.Sprintf("%s-%s.json", label, d.ID))
			if err := os.WriteFile(path, []byte(r.RawModelOutput), 0o644); err != nil {
				log.Printf("[%s] write raw output for %s: %v", label, d.ID, err)
			}
		}
		results = append(results, r)
	}
	return results
}

// improveAndCompare drives the second half of the pipeline: synthesise a
// diff from the baseline mismatches, apply it to a temp copy of the
// prompts dir, re-evaluate the corpus against the patched prompt, and
// decide whether the round-trip improved the score. The returned verdict
// line is what the caller prints under the table(s); the returned
// improved-results slice is nil whenever there was no successful second
// pass to compare against.
//
// The proposed diff is written to diffOutPath whenever the improver
// returns one — including when it fails to apply or regresses the score
// — so the developer can always inspect the suggestion. The verdict line
// is the source of truth for whether the improver actually helped.
func improveAndCompare(
	ctx context.Context,
	backend chatClient,
	promptDir, sysPrompt, userTpl string,
	docs []corpusDoc,
	baseline []evaluationResult,
	baselineMatched, total int,
	diffOutPath string,
) (verdict string, improved []evaluationResult) {
	if baselineMatched == total {
		log.Printf("all heuristics matched expected values; skipping improver call")
		return fmt.Sprintf("Score: %d/%d — already perfect, no improvement attempted.", baselineMatched, total), nil
	}

	log.Printf("synthesising improvement diff")
	sysPath := filepath.Join(promptDir, "document_heuristics_system.txt")
	diff, err := improvePrompt(ctx, backend, sysPath, sysPrompt, baseline)
	if err != nil {
		log.Printf("improver failed: %v", err)
		return fmt.Sprintf("Score: %d/%d — improver call failed: %v", baselineMatched, total, err), nil
	}

	// Persist the model's proposed diff before doing anything else with
	// it. Even diffs that fail to apply or that regress the score are
	// useful for the developer to inspect — they show what the improver
	// is reaching for and where it's going wrong.
	diffWritten := writeDiff(diff, diffOutPath)

	patchedDir, cleanup, err := applyDiffToPromptsCopy(promptDir, diff)
	if err != nil {
		log.Printf("apply diff failed: %v", err)
		return fmt.Sprintf("Score: %d/%d — proposed diff did not apply cleanly (%v); see %s.", baselineMatched, total, err, diffWritten), nil
	}
	defer cleanup()

	patchedSys, patchedUser, err := loadProductionPrompts(patchedDir)
	if err != nil {
		log.Printf("reload patched prompts failed: %v", err)
		return fmt.Sprintf("Score: %d/%d — could not reload patched prompts: %v; see %s.", baselineMatched, total, err, diffWritten), nil
	}

	log.Printf("re-running corpus against patched prompt")
	improved = runCorpus(ctx, backend, patchedSys, patchedUser, docs, "", "improved")
	improvedMatched, _ := totalScore(improved)
	delta := improvedMatched - baselineMatched

	label := "improvement"
	if delta <= 0 {
		label = "no improvement"
	}
	log.Printf("%s: %d → %d matched (delta %+d); diff at %s", label, baselineMatched, improvedMatched, delta, diffWritten)
	return fmt.Sprintf("Score: %d/%d → %d/%d (%+d) — %s; proposed diff at %s.", baselineMatched, total, improvedMatched, total, delta, label, diffWritten), improved
}

// writeDiff writes the proposed diff to path with a trailing newline.
// Returns the path on success, or a "<path> (write failed: …)" string
// on failure so the verdict line reflects what actually happened
// without aborting the pipeline.
func writeDiff(diff, path string) string {
	if err := os.WriteFile(path, []byte(ensureTrailingNewline(diff)), 0o644); err != nil {
		log.Printf("write diff to %s: %v", path, err)
		return fmt.Sprintf("%s (write failed: %v)", path, err)
	}
	log.Printf("wrote proposed diff to %s", path)
	return path
}

func ensureTrailingNewline(s string) string {
	if s == "" || s[len(s)-1] == '\n' {
		return s
	}
	return s + "\n"
}

func buildBackend(useClaude, useOpencode bool) (chatClient, error) {
	switch {
	case useOpencode:
		model := os.Getenv("OPENCODE_MODEL")
		if model == "" {
			return nil, fmt.Errorf("--opencode requires OPENCODE_MODEL to be set")
		}
		bin := os.Getenv("OPENCODE_BIN")
		if bin == "" {
			bin = defaultOpencodeBin
		}
		return &opencodeClient{
			bin:     bin,
			model:   model,
			timeout: timeoutFromEnv("OPENCODE_TIMEOUT_SECONDS"),
		}, nil
	case useClaude:
		apiKey := os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("--claude requires ANTHROPIC_API_KEY to be set")
		}
		baseURL := os.Getenv("CLAUDE_BASE_URL")
		if baseURL == "" {
			baseURL = defaultClaudeBaseURL
		}
		model := os.Getenv("CLAUDE_MODEL")
		if model == "" {
			model = defaultClaudeModel
		}
		return &claudeClient{
			apiKey:    apiKey,
			baseURL:   baseURL,
			model:     model,
			maxTokens: intFromEnv("CLAUDE_MAX_TOKENS", defaultClaudeMaxTok),
			timeout:   timeoutFromEnv("CLAUDE_TIMEOUT_SECONDS"),
		}, nil
	default:
		apiKey := os.Getenv("MAPLE_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("default Maple backend requires MAPLE_API_KEY (or pass --claude or --opencode)")
		}
		baseURL := os.Getenv("MAPLE_BASE_URL")
		if baseURL == "" {
			baseURL = defaultMapleBaseURL
		}
		model := os.Getenv("MAPLE_MODEL")
		if model == "" {
			model = defaultMapleModel
		}
		return &mapleClient{
			baseURL: baseURL,
			apiKey:  apiKey,
			model:   model,
			timeout: timeoutFromEnv("MAPLE_TIMEOUT_SECONDS"),
		}, nil
	}
}

func timeoutFromEnv(name string) time.Duration {
	raw := os.Getenv(name)
	if raw == "" {
		return defaultClientTimeout
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		log.Printf("%s=%q is not a positive integer; using default %s", name, raw, defaultClientTimeout)
		return defaultClientTimeout
	}
	return time.Duration(n) * time.Second
}

func intFromEnv(name string, fallback int) int {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}
