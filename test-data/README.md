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

To rebuild a payload after editing tips:

```sh
for f in test-data/<batch>/tip*.txt; do
  jq -Rs --arg fn "$(basename "$f")" '{filename: $fn, content: .}' "$f"
done | jq -s '{documents: .}' > test-data/<batch>/case-test.json
```
