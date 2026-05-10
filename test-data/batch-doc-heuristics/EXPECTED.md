# Expected: Per-Document Heuristics

Purpose: regression-test the document heuristic prompt. Topic grouping is
secondary for this batch; inspect each document's heuristic ratings through
`/topics/{tid}/documents`.

Expected targets:

- `tip1.txt`: low `consistency`; references should be medium/high
- `tip2.txt`: high `consistency`; references should be high
- `tip3.txt`: low `references`; consistency should be high/medium
- `tip4.txt`: high `references`
- `tip5.txt`: low `emotive_language`
- `tip6.txt`: high `emotive_language`; ideology should stay low/medium
- `tip7.txt`: low `ideology`
- `tip8.txt`: high `ideology`; emotive language should stay low/medium

Checks:

- Every document should return exactly the four document heuristics:
  `consistency`, `references`, `emotive_language`, and `ideology`.
- `facts_to_verify` should contain at most three entries per document.
- The target heuristic should move in the expected direction even if exact
  explanations vary by model.
