# Expected: Filtering Matrix

Purpose: verify document filtering uses only the two negative-signal
per-document heuristics. A document is filtered only when both `emotive_language`
and `ideology` are `medium` or `high`.

Expected topic:

- Wage theft / unpaid overtime at Harborline Logistics: all four documents

Expected document outcomes:

- `tip1.txt`: `emotive_language=low`, `ideology=low`, `filtered=false`
- `tip2.txt`: `emotive_language=high` or `medium`, `ideology=low`, `filtered=false`
- `tip3.txt`: `emotive_language=low`, `ideology=high` or `medium`, `filtered=false`
- `tip4.txt`: `emotive_language=high` or `medium`, `ideology=high` or `medium`, `filtered=true`

Checks:

- All four documents should remain visible in `/topics/{tid}/documents`.
- Only `tip4.txt` should be excluded from the group-heuristic scan.
- The topic should still receive group heuristics because at least three
  unfiltered documents remain.
