# Expected: Large Topic Grouping

Purpose: verify that a single larger dump is grouped into sensible topics rather
than one topic per file or one generic catch-all topic.

Expected topics and document counts:

- Public-sector procurement fraud: `tip1.txt`, `tip4.txt`, `tip8.txt` (3 docs)
- Pharmaceutical safety-data manipulation: `tip2.txt`, `tip7.txt` (2 docs)
- Workplace harassment cover-up: `tip3.txt`, `tip6.txt`, `tip10.txt` (3 docs)
- Environmental discharge / pollution concealment: `tip5.txt`, `tip9.txt` (2 docs)

Checks:

- `GET /cases/{id}` should return 4 topics with document counts `3, 3, 2, 2`.
- Procurement tips should group together despite different agencies, vendors,
  people, and jurisdictions.
- Pharma tips should group together despite different companies and drugs.
- Harassment-cover-up tips should group together based on internal report and
  institutional non-action.
- Environmental tips should group together based on concealed discharge or
  pollution records, not with procurement or pharma compliance topics.
- Topic titles may differ, but the document membership should match the intent.
