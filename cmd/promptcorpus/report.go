package main

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

type reportInput struct {
	Baseline []evaluationResult
	// Improved is the per-document evaluation after the model-proposed
	// diff was applied to a temp copy of the prompt. Nil when no second
	// pass ran (either the baseline was already perfect, or the improver
	// or apply step failed).
	Improved []evaluationResult
	// Verdict is the one-line summary printed under the table(s) — for
	// example "Score: 18/24 → 22/24 (+4) — improvement". The caller
	// composes it so error cases ("diff failed to apply: …") and the
	// happy paths share the same slot.
	Verdict string
}

// renderReport emits the baseline summary table, optionally followed by
// the post-diff summary table, and a verdict line. The model-proposed
// diff is written to its own file by the caller so it can be applied
// directly with "git apply".
func renderReport(in reportInput) string {
	var b strings.Builder

	if in.Improved != nil {
		b.WriteString("## Baseline\n\n")
	}
	writeSummaryTable(&b, in.Baseline)

	if in.Improved != nil {
		b.WriteString("\n## After applying proposed diff\n\n")
		writeSummaryTable(&b, in.Improved)
	}

	if in.Verdict != "" {
		b.WriteString("\n")
		b.WriteString(strings.TrimRight(in.Verdict, "\n"))
		b.WriteString("\n")
	}
	return b.String()
}

func writeSummaryTable(b *strings.Builder, results []evaluationResult) {
	headers := []string{"Document", "Consistency", "References", "Emotive", "Ideology", "Score"}
	rows := make([][]string, 0, len(results))
	for _, r := range results {
		rows = append(rows, []string{
			r.Doc.ID,
			cell(r, "consistency"),
			cell(r, "references"),
			cell(r, "emotive_language"),
			cell(r, "ideology"),
			score(r),
		})
	}

	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = utf8.RuneCountInString(h)
	}
	for _, row := range rows {
		for i, v := range row {
			if w := utf8.RuneCountInString(v); w > widths[i] {
				widths[i] = w
			}
		}
	}

	writeRow(b, headers, widths)
	writeSeparator(b, widths)
	for _, row := range rows {
		writeRow(b, row, widths)
	}
}

func writeRow(b *strings.Builder, cells []string, widths []int) {
	b.WriteByte('|')
	for i, v := range cells {
		fmt.Fprintf(b, " %s%s |", v, strings.Repeat(" ", widths[i]-utf8.RuneCountInString(v)))
	}
	b.WriteByte('\n')
}

func writeSeparator(b *strings.Builder, widths []int) {
	b.WriteByte('|')
	for _, w := range widths {
		b.WriteByte('-')
		b.WriteString(strings.Repeat("-", w))
		b.WriteString("-|")
	}
	b.WriteByte('\n')
}

// score returns "matched/total" for one row, where total is the number
// of heuristics in the document's answer key. Parse failures show as
// "0/N" — 0 right out of N expected — so the column never has to fall
// back to a placeholder.
func score(r evaluationResult) string {
	total := len(r.Doc.AnswerKey.expectations())
	matched := 0
	for _, h := range r.Heuristics {
		if h.Matched {
			matched++
		}
	}
	return fmt.Sprintf("%d/%d", matched, total)
}

func cell(r evaluationResult, name string) string {
	if r.ParseError != "" {
		return "—"
	}
	for _, h := range r.Heuristics {
		if h.Name == name {
			marker := "✗"
			if h.Matched {
				marker = "✓"
			}
			return fmt.Sprintf("%s expected %s, got %s", marker, h.Expected, h.Actual)
		}
	}
	return "—"
}
