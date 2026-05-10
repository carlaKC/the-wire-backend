package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeChat lets us drive the evaluation pipeline deterministically without
// touching a real model. The first response in `responses` is returned for
// the first call, the second for the second, and so on; an empty queue
// produces an error. This shape matches how the pipeline calls the model:
// one document-heuristics call per corpus document, then optionally one
// improver call.
type fakeChat struct {
	responses []string
	calls     []chatRequest
}

func (f *fakeChat) label() string { return "fake" }

func (f *fakeChat) ChatCompletion(_ context.Context, req chatRequest) (string, error) {
	f.calls = append(f.calls, req)
	if len(f.responses) == 0 {
		return "", errAllResponsesConsumed
	}
	resp := f.responses[0]
	f.responses = f.responses[1:]
	return resp, nil
}

var errAllResponsesConsumed = stringError("fake chat: response queue exhausted")

type stringError string

func (e stringError) Error() string { return string(e) }

func TestEvaluateDocumentMarksAllMatchedWhenModelReturnsExpectedRatings(t *testing.T) {
	doc := corpusDoc{
		ID:      "tip1",
		DocPath: "tip1.txt",
		Text:    "document body",
		AnswerKey: answerKey{
			Document:        "tip1.txt",
			Consistency:     expectedHeuristic{Expected: "low", Explanation: "two contradictions"},
			References:      expectedHeuristic{Expected: "high", Explanation: "lots of refs"},
			EmotiveLanguage: expectedHeuristic{Expected: "low", Explanation: "calm"},
			Ideology:        expectedHeuristic{Expected: "low", Explanation: "factual"},
		},
	}

	fc := &fakeChat{responses: []string{`{
        "heuristics": [
            {"name": "consistency", "score": "low", "explanation": "found two"},
            {"name": "references", "score": "high", "explanation": "many"},
            {"name": "emotive_language", "score": "low", "explanation": "calm"},
            {"name": "ideology", "score": "low", "explanation": "neutral"}
        ]
    }`}}

	r := evaluateDocument(context.Background(), fc, "system", "USER {{DOCUMENT_TEXT}}", doc)
	if r.ParseError != "" {
		t.Fatalf("ParseError = %q, want empty", r.ParseError)
	}
	if !r.allMatched() {
		t.Fatalf("allMatched() = false; per-heuristic outcomes: %#v", r.Heuristics)
	}
}

func TestEvaluateDocumentSurfacesMismatches(t *testing.T) {
	doc := corpusDoc{
		ID:   "tip1",
		Text: "body",
		AnswerKey: answerKey{
			Consistency:     expectedHeuristic{Expected: "low", Explanation: "should be low"},
			References:      expectedHeuristic{Expected: "high", Explanation: "rich"},
			EmotiveLanguage: expectedHeuristic{Expected: "low"},
			Ideology:        expectedHeuristic{Expected: "low"},
		},
	}
	fc := &fakeChat{responses: []string{`{
        "heuristics": [
            {"name": "consistency", "score": "high", "explanation": "looked fine"},
            {"name": "references", "score": "high", "explanation": "ok"},
            {"name": "emotive_language", "score": "low", "explanation": "ok"},
            {"name": "ideology", "score": "low", "explanation": "ok"}
        ]
    }`}}

	r := evaluateDocument(context.Background(), fc, "system", "USER {{DOCUMENT_TEXT}}", doc)
	mm := mismatchedHeuristics(r)
	if len(mm) != 1 {
		t.Fatalf("mismatch count = %d, want 1; outcomes: %#v", len(mm), r.Heuristics)
	}
	if mm[0].Name != "consistency" || mm[0].Expected != "low" || mm[0].Actual != "high" {
		t.Fatalf("unexpected mismatch: %#v", mm[0])
	}
}

func TestRecountDiffHunksFixesUndercountedDest(t *testing.T) {
	// Hunk 1 says +28,19 but the body actually has 20 destination lines
	// (3 context + 14 added + 3 context). The cumulative miscount cascades
	// into hunk 2's dest start, which the model wrote as +51 but should be
	// +52 once hunk 1 is corrected.
	in := strings.Join([]string{
		"--- a/prompts/document_heuristics_system.txt",
		"+++ b/prompts/document_heuristics_system.txt",
		"@@ -28,6 +28,19 @@",
		" line A",
		" line B",
		" line C",
		"+added 1",
		"+added 2",
		"+added 3",
		"+added 4",
		"+added 5",
		"+added 6",
		"+added 7",
		"+added 8",
		"+added 9",
		"+added 10",
		"+added 11",
		"+added 12",
		"+added 13",
		"+added 14",
		" line D",
		" line E",
		" line F",
		"@@ -38,6 +51,12 @@",
		" line G",
		" line H",
		" line I",
		"+x1",
		"+x2",
		"+x3",
		"+x4",
		"+x5",
		"+x6",
		" line J",
		" line K",
		" line L",
		"",
	}, "\n")

	got := recountDiffHunks(in)

	if !strings.Contains(got, "@@ -28,6 +28,20 @@") {
		t.Errorf("hunk 1 header not corrected to +28,20:\n%s", got)
	}
	if !strings.Contains(got, "@@ -38,6 +52,12 @@") {
		t.Errorf("hunk 2 header not corrected to +52,12:\n%s", got)
	}
	// Round-tripping through recount must not add or drop body lines.
	if strings.Count(got, "\n") != strings.Count(in, "\n") {
		t.Errorf("line count changed (in=%d, out=%d):\n%s", strings.Count(in, "\n"), strings.Count(got, "\n"), got)
	}
	if !strings.Contains(got, "+added 14") || !strings.Contains(got, "+x6") {
		t.Errorf("hunk body lost lines:\n%s", got)
	}
}

func TestRecountDiffHunksLeavesAlreadyCorrectHunksAlone(t *testing.T) {
	in := "--- a/x\n+++ b/x\n@@ -1,3 +1,3 @@\n line\n-old\n+new\n line\n"
	got := recountDiffHunks(in)
	if got != in {
		t.Errorf("already-correct hunk modified:\nwant:\n%s\ngot:\n%s", in, got)
	}
}

func TestRenderReportEmitsTableWithScoreColumn(t *testing.T) {
	results := []evaluationResult{
		{
			Doc: corpusDoc{ID: "tip1", DocPath: "tip1.txt"},
			Heuristics: []heuristicOutcome{
				{Name: "consistency", Expected: "low", Actual: "high", Matched: false, ReviewerRationale: "two contradictions exist", ModelExplanation: "looked fine"},
				{Name: "references", Expected: "high", Actual: "high", Matched: true},
				{Name: "emotive_language", Expected: "low", Actual: "low", Matched: true},
				{Name: "ideology", Expected: "low", Actual: "low", Matched: true},
			},
		},
		{
			Doc:        corpusDoc{ID: "tip2", DocPath: "tip2.txt"},
			ParseError: "decode JSON: unexpected token",
		},
	}

	report := renderReport(reportInput{Baseline: results})

	// Find the row for each tip and assert its score cell.
	tip1Row := lineContaining(report, "| tip1 ")
	if tip1Row == "" {
		t.Fatalf("report missing tip1 row:\n%s", report)
	}
	if !strings.Contains(tip1Row, "3/4") {
		t.Errorf("tip1 score cell expected 3/4, got row:\n%s", tip1Row)
	}

	tip2Row := lineContaining(report, "| tip2 ")
	if tip2Row == "" {
		t.Fatalf("report missing tip2 row:\n%s", report)
	}
	if !strings.Contains(tip2Row, "0/4") {
		t.Errorf("tip2 (parse error) score cell expected 0/4, got row:\n%s", tip2Row)
	}

	for _, want := range []string{
		"| Document",
		"| Score",
		"✗ expected low, got high",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("report missing %q:\n%s", want, report)
		}
	}
	for _, unwanted := range []string{
		"## Mismatches",
		"## Suggested prompt improvements",
		"--- a/",
		"+++ b/",
		"@@",
		"```",
		"Notes",
		"model output unparseable",
	} {
		if strings.Contains(report, unwanted) {
			t.Errorf("report should not contain %q:\n%s", unwanted, report)
		}
	}
}

func lineContaining(s, substr string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, substr) {
			return line
		}
	}
	return ""
}

func TestRenderReportPadsColumnsToEqualWidth(t *testing.T) {
	results := []evaluationResult{
		{
			Doc: corpusDoc{ID: "tip1", DocPath: "tip1.txt"},
			Heuristics: []heuristicOutcome{
				{Name: "consistency", Expected: "low", Actual: "high", Matched: false},
				{Name: "references", Expected: "high", Actual: "high", Matched: true},
				{Name: "emotive_language", Expected: "low", Actual: "low", Matched: true},
				{Name: "ideology", Expected: "low", Actual: "low", Matched: true},
			},
		},
		{
			Doc: corpusDoc{ID: "tip-with-a-much-longer-id", DocPath: "tip6.txt"},
			Heuristics: []heuristicOutcome{
				{Name: "consistency", Expected: "high", Actual: "high", Matched: true},
				{Name: "references", Expected: "high", Actual: "high", Matched: true},
				{Name: "emotive_language", Expected: "low", Actual: "low", Matched: true},
				{Name: "ideology", Expected: "low", Actual: "low", Matched: true},
			},
		},
	}

	report := renderReport(reportInput{Baseline: results})

	rows := strings.Split(strings.TrimRight(report, "\n"), "\n")
	if len(rows) < 4 {
		t.Fatalf("expected at least header + separator + 2 data rows, got %d rows:\n%s", len(rows), report)
	}
	// Pipe characters must align across header + separator + every data row.
	want := pipePositions(rows[0])
	for i, row := range rows {
		got := pipePositions(row)
		if !slicesEqual(got, want) {
			t.Errorf("row %d pipes %v don't match header pipes %v\n  row: %q", i, got, want, row)
		}
	}
}

// pipePositions returns the rune index (not byte offset) of each '|' in
// s. Rune indexing is what determines visual alignment in a monospace
// renderer, since multi-byte runes like ✓/✗ each occupy one column but
// three bytes.
func pipePositions(s string) []int {
	var out []int
	idx := 0
	for _, r := range s {
		if r == '|' {
			out = append(out, idx)
		}
		idx++
	}
	return out
}

func TestTotalScoreSumsMatchesAcrossDocs(t *testing.T) {
	mkAnswerKey := func() answerKey {
		return answerKey{
			Consistency:     expectedHeuristic{Expected: "high"},
			References:      expectedHeuristic{Expected: "high"},
			EmotiveLanguage: expectedHeuristic{Expected: "low"},
			Ideology:        expectedHeuristic{Expected: "low"},
		}
	}

	// 3/4 + 4/4 + 0/4 (parse error) = 7/12.
	results := []evaluationResult{
		{
			Doc: corpusDoc{ID: "tip1", AnswerKey: mkAnswerKey()},
			Heuristics: []heuristicOutcome{
				{Matched: false}, {Matched: true}, {Matched: true}, {Matched: true},
			},
		},
		{
			Doc: corpusDoc{ID: "tip2", AnswerKey: mkAnswerKey()},
			Heuristics: []heuristicOutcome{
				{Matched: true}, {Matched: true}, {Matched: true}, {Matched: true},
			},
		},
		{
			Doc:        corpusDoc{ID: "tip3", AnswerKey: mkAnswerKey()},
			ParseError: "decode JSON: nope",
		},
	}

	matched, total := totalScore(results)
	if matched != 7 || total != 12 {
		t.Fatalf("totalScore = (%d, %d), want (7, 12)", matched, total)
	}
}

func TestRenderReportIncludesBothTablesAndVerdictWhenImproved(t *testing.T) {
	mkRow := func(id string, consistencyMatched bool) evaluationResult {
		return evaluationResult{
			Doc: corpusDoc{ID: id, DocPath: id + ".txt"},
			Heuristics: []heuristicOutcome{
				{Name: "consistency", Expected: "low", Actual: ifElse(consistencyMatched, "low", "high"), Matched: consistencyMatched},
				{Name: "references", Expected: "high", Actual: "high", Matched: true},
				{Name: "emotive_language", Expected: "low", Actual: "low", Matched: true},
				{Name: "ideology", Expected: "low", Actual: "low", Matched: true},
			},
		}
	}
	baseline := []evaluationResult{mkRow("tip1", false), mkRow("tip2", true)}
	improved := []evaluationResult{mkRow("tip1", true), mkRow("tip2", true)}

	report := renderReport(reportInput{
		Baseline: baseline,
		Improved: improved,
		Verdict:  "Score: 7/8 → 8/8 (+1) — improvement, diff written to document_heuristics_system.diff.",
	})

	for _, want := range []string{
		"## Baseline",
		"## After applying proposed diff",
		"Score: 7/8 → 8/8 (+1) — improvement",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("report missing %q:\n%s", want, report)
		}
	}
	// Header line "## Baseline" must come before "## After applying".
	if strings.Index(report, "## Baseline") >= strings.Index(report, "## After applying") {
		t.Errorf("Baseline section should precede After section:\n%s", report)
	}
}

func TestRenderReportSkipsBaselineHeaderWhenNoSecondPass(t *testing.T) {
	results := []evaluationResult{
		{
			Doc: corpusDoc{ID: "tip1", DocPath: "tip1.txt"},
			Heuristics: []heuristicOutcome{
				{Name: "consistency", Expected: "low", Actual: "low", Matched: true},
				{Name: "references", Expected: "high", Actual: "high", Matched: true},
				{Name: "emotive_language", Expected: "low", Actual: "low", Matched: true},
				{Name: "ideology", Expected: "low", Actual: "low", Matched: true},
			},
		},
	}

	report := renderReport(reportInput{
		Baseline: results,
		Verdict:  "Score: 4/4 — already perfect, no improvement attempted.",
	})

	if strings.Contains(report, "## Baseline") {
		t.Errorf("did not expect '## Baseline' header when there is no second pass:\n%s", report)
	}
	if strings.Contains(report, "## After applying") {
		t.Errorf("did not expect 'After applying' section when there is no second pass:\n%s", report)
	}
	if !strings.Contains(report, "already perfect") {
		t.Errorf("verdict line missing:\n%s", report)
	}
}

func TestApplyDiffToPromptsCopyAppliesPatchToCopy(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available in PATH")
	}

	srcDir := t.TempDir()
	original := "line one\nline two\nline three\n"
	srcFile := filepath.Join(srcDir, "document_heuristics_system.txt")
	if err := os.WriteFile(srcFile, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	diff := strings.Join([]string{
		"--- a/prompts/document_heuristics_system.txt",
		"+++ b/prompts/document_heuristics_system.txt",
		"@@ -1,3 +1,3 @@",
		" line one",
		"-line two",
		"+line two PATCHED",
		" line three",
		"",
	}, "\n")

	patchedDir, cleanup, err := applyDiffToPromptsCopy(srcDir, diff)
	if err != nil {
		t.Fatalf("applyDiffToPromptsCopy: %v", err)
	}
	defer cleanup()

	got, err := os.ReadFile(filepath.Join(patchedDir, "document_heuristics_system.txt"))
	if err != nil {
		t.Fatal(err)
	}
	wantPatched := "line one\nline two PATCHED\nline three\n"
	if string(got) != wantPatched {
		t.Errorf("patched file content = %q, want %q", got, wantPatched)
	}

	// Original must be untouched.
	gotOrig, err := os.ReadFile(srcFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotOrig) != original {
		t.Errorf("source file was modified — should have stayed at %q, got %q", original, gotOrig)
	}
}

func TestApplyDiffToPromptsCopyReportsApplyFailure(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available in PATH")
	}

	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "document_heuristics_system.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Hunk context doesn't match anything in the source file → git apply rejects.
	bogus := strings.Join([]string{
		"--- a/prompts/document_heuristics_system.txt",
		"+++ b/prompts/document_heuristics_system.txt",
		"@@ -1,1 +1,1 @@",
		"-something else entirely",
		"+replacement",
		"",
	}, "\n")

	if _, _, err := applyDiffToPromptsCopy(srcDir, bogus); err == nil {
		t.Fatalf("expected error from non-applying diff, got nil")
	}
}

// LLMs sometimes emit one diff with two "--- a/<file>" sections for the
// same file (alternate phrasings of the same edit). git apply rejects
// the combined blob as "corrupt patch". The implementation must split
// on file-header boundaries, apply each section in turn, and tolerate a
// section that no longer applies because an earlier section has already
// landed the change.
func TestApplyDiffToPromptsCopyHandlesDuplicateFileHeaders(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available in PATH")
	}

	srcDir := t.TempDir()
	original := "alpha\nbeta\ngamma\n"
	srcFile := filepath.Join(srcDir, "document_heuristics_system.txt")
	if err := os.WriteFile(srcFile, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	// Two file-header blocks for the same file. The first inserts a new
	// line; the second tries to land the same edit a different way and
	// will conflict with the post-first-section state. Apply must
	// succeed because the first section landed.
	diff := strings.Join([]string{
		"--- a/prompts/document_heuristics_system.txt",
		"+++ b/prompts/document_heuristics_system.txt",
		"@@ -1,3 +1,4 @@",
		" alpha",
		"+inserted",
		" beta",
		" gamma",
		"--- a/prompts/document_heuristics_system.txt",
		"+++ b/prompts/document_heuristics_system.txt",
		"@@ -1,3 +1,4 @@",
		" alpha",
		"-beta",
		"+beta MODIFIED",
		" gamma",
		"",
	}, "\n")

	patchedDir, cleanup, err := applyDiffToPromptsCopy(srcDir, diff)
	if err != nil {
		t.Fatalf("applyDiffToPromptsCopy: %v", err)
	}
	defer cleanup()

	got, err := os.ReadFile(filepath.Join(patchedDir, "document_heuristics_system.txt"))
	if err != nil {
		t.Fatal(err)
	}
	want := "alpha\ninserted\nbeta\ngamma\n"
	if string(got) != want {
		t.Errorf("patched file = %q, want %q (first section should land, second should be skipped)", got, want)
	}
}

func ifElse(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

func slicesEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
