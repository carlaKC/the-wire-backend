package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

//go:embed prompts/improver_system.txt
var improverSystemPrompt string

// evaluationResult captures the outcome of running the production prompt
// against one corpus document and comparing the four returned heuristics
// against the answer key.
type evaluationResult struct {
	Doc            corpusDoc
	RawModelOutput string
	Heuristics     []heuristicOutcome
	ParseError     string
}

// heuristicOutcome records, for one named heuristic, the value the reviewer
// expected, the value the model returned, the reviewer's explanation, and
// whether they match.
type heuristicOutcome struct {
	Name              string
	Expected          string
	Actual            string
	Matched           bool
	ReviewerRationale string
	ModelExplanation  string
}

// allMatched reports whether every heuristic in the result agreed with the
// answer key. The improver is only called when at least one document has a
// mismatch — agreeing on every heuristic across the corpus is "all good"
// and there is nothing to suggest improving.
func (r evaluationResult) allMatched() bool {
	if r.ParseError != "" {
		return false
	}
	for _, h := range r.Heuristics {
		if !h.Matched {
			return false
		}
	}
	return true
}

// totalScore returns (matched, total) summed across every result. The
// total is taken from each document's answer-key cardinality so a result
// with a parse error still contributes its full denominator — that way
// "the model failed to produce parseable output" counts as a regression
// against the same scoring scale as a partial match.
func totalScore(results []evaluationResult) (matched, total int) {
	for _, r := range results {
		total += len(r.Doc.AnswerKey.expectations())
		for _, h := range r.Heuristics {
			if h.Matched {
				matched++
			}
		}
	}
	return
}

// evaluateDocument runs the production prompt against one document and
// compares each of the four returned heuristic ratings against the answer
// key. Failure modes (model call error, unparseable JSON, missing
// heuristic) become fields on the returned struct so the caller can keep
// processing the rest of the corpus.
func evaluateDocument(ctx context.Context, c chatClient, sysPrompt, userTpl string, doc corpusDoc) evaluationResult {
	res := evaluationResult{Doc: doc}

	output, err := c.ChatCompletion(ctx, chatRequest{
		Temperature:    0,
		ResponseFormat: responseFormat{Type: "json_object"},
		Messages: []chatMessage{
			{Role: "system", Content: sysPrompt},
			{Role: "user", Content: renderUserPrompt(userTpl, doc.Text)},
		},
	})
	if err != nil {
		res.ParseError = "model call failed: " + err.Error()
		return res
	}
	res.RawModelOutput = output

	actuals, err := parseHeuristics(output)
	if err != nil {
		res.ParseError = err.Error()
		return res
	}

	for _, exp := range doc.AnswerKey.expectations() {
		actualRating, actualExpl := actuals[exp.Name].score, actuals[exp.Name].explanation
		res.Heuristics = append(res.Heuristics, heuristicOutcome{
			Name:              exp.Name,
			Expected:          exp.Expected.Expected,
			Actual:            actualRating,
			Matched:           strings.EqualFold(exp.Expected.Expected, actualRating),
			ReviewerRationale: exp.Expected.Explanation,
			ModelExplanation:  actualExpl,
		})
	}
	return res
}

type heuristicScore struct {
	score       string
	explanation string
}

// parseHeuristics decodes the production prompt's JSON output and returns a
// map keyed by heuristic name. Tolerates the same prose-around-JSON drift
// backend/parsing.go has to handle.
func parseHeuristics(output string) (map[string]heuristicScore, error) {
	jsonText := output
	if i := strings.Index(jsonText, "{"); i > 0 {
		jsonText = jsonText[i:]
	}
	dec := json.NewDecoder(strings.NewReader(jsonText))
	var raw struct {
		Heuristics []struct {
			Name        string `json:"name"`
			Score       string `json:"score"`
			Explanation string `json:"explanation"`
		} `json:"heuristics"`
	}
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("parse model JSON: %w", err)
	}
	out := make(map[string]heuristicScore, len(raw.Heuristics))
	for _, h := range raw.Heuristics {
		out[strings.ToLower(strings.TrimSpace(h.Name))] = heuristicScore{
			score:       strings.ToLower(strings.TrimSpace(h.Score)),
			explanation: strings.TrimSpace(h.Explanation),
		}
	}
	return out, nil
}

// improvePrompt asks the model to propose a unified diff against the
// production system prompt given every mismatch we observed across the
// corpus. The prompt is only sent if there is at least one mismatch — the
// caller is expected to skip the call when every document agrees with its
// answer key.
//
// The model's raw output runs through recountDiffHunks before being
// returned, because LLMs routinely produce diffs whose @@ counts disagree
// with the actual hunk body — which makes "git apply" reject the patch.
func improvePrompt(ctx context.Context, c chatClient, promptPath, sysPrompt string, results []evaluationResult) (string, error) {
	digest := buildMismatchDigest(results)
	user := strings.NewReplacer(
		"{{CURRENT_PROMPT}}", sysPrompt,
		"{{PROMPT_PATH}}", promptPath,
		"{{MISMATCH_DIGEST}}", digest,
	).Replace(improverUserTemplate)

	raw, err := c.ChatCompletion(ctx, chatRequest{
		Temperature: 0,
		Messages: []chatMessage{
			{Role: "system", Content: improverSystemPrompt},
			{Role: "user", Content: user},
		},
	})
	if err != nil {
		return "", err
	}
	return recountDiffHunks(raw), nil
}

var hunkHeaderRe = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@(.*)$`)

// recountDiffHunks rewrites the line-count fields and destination start
// positions in each unified-diff hunk header so they match the actual
// hunk body. Source-side start positions are left as the model wrote
// them; "git apply" uses the surrounding context lines to locate the
// hunk regardless of the start line, so a slightly wrong source start is
// recoverable but a wrong count is fatal.
func recountDiffHunks(diff string) string {
	lines := strings.Split(diff, "\n")
	out := make([]string, 0, len(lines))
	cumulativeOffset := 0

	for i := 0; i < len(lines); {
		m := hunkHeaderRe.FindStringSubmatch(lines[i])
		if m == nil {
			out = append(out, lines[i])
			i++
			continue
		}
		srcStart, _ := strconv.Atoi(m[1])
		suffix := m[3]
		srcCount, dstCount, end := countHunkBody(lines, i+1)
		dstStart := srcStart + cumulativeOffset
		out = append(out, fmt.Sprintf("@@ -%d,%d +%d,%d @@%s", srcStart, srcCount, dstStart, dstCount, suffix))
		out = append(out, lines[i+1:end]...)
		cumulativeOffset += dstCount - srcCount
		i = end
	}
	return strings.Join(out, "\n")
}

// countHunkBody walks the lines of one hunk body (lines after a @@
// header) and returns how many source lines, how many destination
// lines, and the index of the first line that is not part of the body.
// The body ends at EOF, the next "@@" header, the next "--- " file
// marker, or any line whose first byte is not one of " ", "+", "-",
// "\\" — including a truly empty line.
func countHunkBody(lines []string, start int) (srcCount, dstCount, end int) {
	end = start
	for end < len(lines) {
		bl := lines[end]
		if bl == "" {
			break
		}
		if strings.HasPrefix(bl, "@@") || strings.HasPrefix(bl, "--- ") {
			break
		}
		switch bl[0] {
		case ' ':
			srcCount++
			dstCount++
		case '-':
			srcCount++
		case '+':
			dstCount++
		case '\\':
		default:
			return
		}
		end++
	}
	return
}

const improverUserTemplate = `Current production prompt (full system text):
"""
{{CURRENT_PROMPT}}
"""

We ran this prompt against a hand-curated corpus and observed the following mismatches between the reviewer's expected heuristic ratings and the model's actual output.

{{MISMATCH_DIGEST}}

Output ONLY a unified diff that, when applied with "git apply -p1" from the repository root, modifies the production prompt above to produce the expected ratings on the documents shown. Use exactly this format:

--- a/{{PROMPT_PATH}}
+++ b/{{PROMPT_PATH}}
@@ -L,N +L,N @@
 unchanged context line
-removed line
+added line
 unchanged context line

Rules:
- Include 3+ lines of unchanged context around each change so the patch tool can locate the hunk even if line numbers drift.
- Multiple hunks are fine; one per logical change.
- Do not output any prose, headers, commentary, or markdown code fences. The first line of your reply must be "--- a/{{PROMPT_PATH}}".`

func buildMismatchDigest(results []evaluationResult) string {
	var b strings.Builder
	for _, r := range results {
		if r.ParseError != "" {
			fmt.Fprintf(&b, "## %s (%s)\n", r.Doc.ID, r.Doc.DocPath)
			fmt.Fprintf(&b, "MODEL ERROR: %s\n\n", r.ParseError)
			continue
		}
		mismatched := mismatchedHeuristics(r)
		if len(mismatched) == 0 {
			continue
		}
		fmt.Fprintf(&b, "## %s (%s)\n", r.Doc.ID, r.Doc.DocPath)
		fmt.Fprintf(&b, "Document text:\n\"\"\"\n%s\n\"\"\"\n\n", strings.TrimRight(r.Doc.Text, "\n"))
		for _, h := range mismatched {
			fmt.Fprintf(&b, "Heuristic: %s\n", h.Name)
			fmt.Fprintf(&b, "  Expected: %s\n", h.Expected)
			fmt.Fprintf(&b, "  Reviewer reasoning: %s\n", h.ReviewerRationale)
			fmt.Fprintf(&b, "  Actual: %s\n", h.Actual)
			fmt.Fprintf(&b, "  Model explanation: %s\n\n", h.ModelExplanation)
		}
	}
	return b.String()
}

func mismatchedHeuristics(r evaluationResult) []heuristicOutcome {
	out := make([]heuristicOutcome, 0)
	for _, h := range r.Heuristics {
		if !h.Matched {
			out = append(out, h)
		}
	}
	return out
}
