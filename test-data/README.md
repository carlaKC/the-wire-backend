# Test Data Batches

These batches are manual regression fixtures for live prompt behavior. Each batch
contains `tip*.txt` source files, an assembled `case-test.json` request body, and
an `EXPECTED.md` file describing the behavior to check after submitting the case.

Run a batch against a live server:

```sh
curl -X POST http://localhost:8080/api/v1/cases \
  -H 'Content-Type: application/json' \
  -d @test-data/batch-topic-grouping-large/case-test.json
```

Poll `GET /api/v1/cases/{id}` until `status` is `complete`, then inspect topic
details and topic documents. Expected topic titles do not need to match exactly;
the important checks are semantic grouping, document membership, filtered flags,
and target heuristic ratings.

## Batches

- `batch-topic-grouping-large`: ten files across four unrelated subject areas.
  Tests whether the case-classification prompt groups many documents into
  sensible topics.
- `batch-filtering-matrix`: four files in one wage-theft topic, covering each
  combination of the two negative document heuristics.
- `batch-group-corroboration`: three independent views of the same revenue
  recognition scheme. Tests group corroboration and shared references.
- `batch-group-contested-narrative`: conflicting accounts of the same workplace
  incident. Tests contested narrative detection.
- `batch-doc-heuristics`: eight single-document fixtures targeting high/low
  ratings for the four per-document heuristics.

## Pitch demo cases

These directories are for live pitch demos rather than regression. Each
includes individual `tipN.txt` files (for drag-and-drop upload through the
front end), an assembled `case-test.json`, an `EXPECTED.md` describing what
the system should produce, and a `PITCH.md` with the on-stage narration.

- `demo-categorization`: ten tips splitting cleanly into two topics
  (defense-contractor financial misconduct, pharmaceutical data integrity).
  Pitch beat: read at scale and group correctly without human guidance.
- `demo-contradictions`: three tips. Two have seeded internal inconsistencies
  (temporal+numerical, role/access+identity drift). One is a clean control
  with apparent inconsistencies that resolve on careful reading. Pitch beat:
  the system reads, it doesn't just pattern-match.
- `demo-corroboration`: three independent vantage points on the same medical
  device kickback scheme (manufacturer compliance officer, competitor sales
  rep, teaching-hospital fellow). Pitch beat: independent corroboration is
  what turns a tip into a story.
- `demo-evidence-quality`: two tips about the same allegation against the
  same target. One is a first-person source with specific transactions,
  dates, and folio numbers; the other is rumor-shaped with no documents.
  Pitch beat: not all tips are equally reportable, and the system grades
  evidence accordingly.

To rebuild a payload after editing tips:

```sh
for f in test-data/<batch>/tip*.txt; do
  jq -Rs --arg fn "$(basename "$f")" '{filename: $fn, content: .}' "$f"
done | jq -s '{documents: .}' > test-data/<batch>/case-test.json
```
