# The Wire — Document Grading API

HTTP API for submitting whistleblower document dumps and retrieving categorized, heuristic-graded results. Currently returns mocked data; the contract is stable, the values are placeholders.

## Running the server

```sh
go run .
```

Listens on `:8080` by default (override with `PORT`).

CORS is permissive (`Access-Control-Allow-Origin: *`) — the API can be called from any frontend origin without a proxy.

All requests and responses are `application/json` (UTF-8).

## Health check

`GET /healthcheck` → `{"time": "<RFC3339 UTC>"}`. Sits outside the `/api/v1` prefix; use it to verify the server is up.

## Quickstart

```sh
# 1. Create a case
curl -X POST http://localhost:8080/api/v1/cases \
  -H 'Content-Type: application/json' \
  -d '{"documents":[{"filename":"memo.txt","content":"…"},{"filename":"email.txt","content":"…"}]}'
# → { "case_id": 1 }

# 2. List categories
curl http://localhost:8080/api/v1/cases/1

# 3. Category detail (heuristics for the category as a whole)
curl http://localhost:8080/api/v1/cases/1/categories/1

# 4. Documents in a category (raw content + per-document heuristics)
curl http://localhost:8080/api/v1/cases/1/categories/1/documents
```

## Concepts

- **Case** — one document dump. Created by POSTing a set of text documents. Identified by an integer.
- **Category** — a grouping the service infers from the dump (e.g. "Financial irregularities"). A case has 1–N categories. Categories with no assigned documents are not returned.
- **Document** — one text file from the original submission. Each document belongs to exactly one category.
- **Heuristic** — a named graded signal: `{ name, rating, description }`. Appears at the **category level** (signal across the whole category) and the **document level** (signal for that one document). The set of heuristic names is **open**: render whatever the API returns rather than hardcoding the list. Today you'll see `consistency`, `references`, `red_flags`, and (on categories) `language_signals`. More may be added.

## Enumerations

- **Triage** (category-level): `"high" | "medium" | "low"`. Use for visual emphasis — high = alert, medium = caution, low = informational.
- **Heuristic rating**: `"high" | "medium" | "low"`. **Polarity is heuristic-specific**:
  - `consistency`, `references`, `language_signals` → high is *positive* (strong corroborating signal).
  - `red_flags` → high is *negative* (more red flags spotted).
  - For unknown heuristic names, do not assume a polarity; render the rating neutrally and rely on the `description`.

## Processing model

`POST /cases` returns the case ID immediately. Analysis runs asynchronously, and the case progresses through `status` values on the case summary:

| Status | Meaning |
|--------|---------|
| `processing` | Analysis is in flight. `categories` may be empty or partial. Per-document `heuristics` may be empty. Category endpoints (`/categories/{cid}` and `/categories/{cid}/documents`) return `404 category_not_found` until the category is materialized. |
| `complete` | Analysis finished. All data populated. |
| `failed` | Analysis errored. The case is terminal; further polling will not change the result. |

Clients should poll `GET /cases/{id}` until `status` is `complete` or `failed`.

The current mock implementation is effectively synchronous: every newly created case is reported with `status: "complete"` immediately.

## Mock dataset

The current implementation is **fully mocked** — every response is hardcoded.

- `POST /cases` validates the request body shape (must contain at least one document with non-empty `content`) but otherwise ignores the input. It always returns `case_id: 1`.
- Only `case_id = 1` exists. Any other ID returns `case_not_found`.
- Case 1 has exactly 3 categories with IDs `1`, `2`, `3`, and 4 documents distributed across them (2 / 1 / 1). Other category IDs return `category_not_found`.
- `created_at` is hardcoded to `2026-05-09T12:34:56Z`.
- The dataset tells a coherent fictional scenario about undisclosed payments to "Atlas Holdings" — including a financial memo, a suspicious invoice with `red_flags: high`, an internal email proposing to move conversations off-record, and a formal compliance letter that contradicts the other documents (`consistency: low`). Useful for end-to-end demos.

Real grading and persistence are not yet implemented. The wire shape is stable.

---

## Endpoints

Base path: `/api/v1`

### POST `/cases`

Create a case from a set of text documents.

**Request body:**
```json
{
  "documents": [
    { "filename": "memo-2025-04.txt", "content": "Internal memo re: Q2…" },
    { "filename": "email-thread.txt", "content": "From: alice@…" }
  ]
}
```

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `documents` | array | yes | Must contain at least 1 entry. |
| `documents[].content` | string | yes | Non-empty. The raw text of the document. |
| `documents[].filename` | string | no | Optional. The mock implementation ignores the request body beyond shape validation. |

**Response 201:**
```json
{ "case_id": 1 }
```

**Errors:**
- `400 invalid_request` — body is not valid JSON, `documents` is missing/empty, or any entry has empty `content`.

---

### GET `/cases/{case_id}`

Case summary — the categories the service inferred for this dump.

**Response 200** (current mock returns this exactly):
```json
{
  "case_id": 1,
  "created_at": "2026-05-09T12:34:56Z",
  "status": "complete",
  "document_count": 4,
  "categories": [
    {
      "id": 1,
      "title": "Financial irregularities",
      "triage": "high",
      "description": "Documents reference off-book payments to Atlas Holdings and Northbridge Consulting that lack standard procurement approvals."
    },
    {
      "id": 2,
      "title": "Internal communications",
      "triage": "medium",
      "description": "Email correspondence between staff discussing the Atlas payments and proposing to move further conversations off-record."
    },
    {
      "id": 3,
      "title": "External correspondence",
      "triage": "low",
      "description": "Formal disclosures to external compliance auditors stating no off-book arrangements exist."
    }
  ]
}
```

| Field | Type | Notes |
|-------|------|-------|
| `created_at` | string | RFC3339 UTC. |
| `status` | string | One of `processing`, `complete`, `failed`. See [Processing model](#processing-model). |
| `document_count` | integer | Total documents in the case (sum across categories). |
| `categories` | array | Order is service-defined; sort on the client if you need triage-first ordering. May be empty while `status` is `processing`. |

**Errors:**
- `404 case_not_found`.

---

### GET `/cases/{case_id}/categories/{category_id}`

Category detail with category-level heuristics.

**Response 200** (example: `category_id = 1` — Financial irregularities):
```json
{
  "case_id": 1,
  "category": {
    "id": 1,
    "title": "Financial irregularities",
    "triage": "high",
    "description": "Documents reference off-book payments to Atlas Holdings and Northbridge Consulting that lack standard procurement approvals.",
    "document_count": 2,
    "heuristics": [
      { "name": "consistency",      "rating": "high",   "description": "Dates, amounts, and counterparty names corroborate across the memo and the invoice." },
      { "name": "references",       "rating": "medium", "description": "Atlas Holdings and Northbridge Consulting both appear in public business registries; the cited PO number could not be verified." },
      { "name": "red_flags",        "rating": "high",   "description": "One document explicitly instructs the recipient not to share with audit, and another lists 'off-book' as a line item." },
      { "name": "language_signals", "rating": "medium", "description": "Tone and terminology are consistent with internal finance correspondence at this organization." }
    ]
  }
}
```

`heuristics` is an open list — render every entry the server returns. Don't assume a fixed length or fixed names.

**Errors:**
- `404 case_not_found`, `404 category_not_found`.

---

### GET `/cases/{case_id}/categories/{category_id}/documents`

The documents in a category, with their raw content and per-document heuristics.

**Response 200** (example: `category_id = 1`, abbreviated — `content` is multi-line plain text):
```json
{
  "case_id": 1,
  "category_id": 1,
  "documents": [
    {
      "id": 1,
      "filename": "memo-2025-04-15.txt",
      "content": "INTERNAL MEMO\n\nDate: 2025-04-15\nFrom: J. Doe, Finance\nTo:   M. Smith, Procurement\n\nRe: Q2 off-cycle vendor disbursements\n…",
      "heuristics": [
        { "name": "consistency", "rating": "high",   "description": "Dates and counterparties match the Atlas invoice in this category." },
        { "name": "references",  "rating": "medium", "description": "Atlas Holdings and Northbridge Consulting are verifiable in public registries; PO #84221 is not." },
        { "name": "red_flags",   "rating": "low",    "description": "Format, header, and tone match other internal memos from this organization." }
      ]
    },
    {
      "id": 2,
      "filename": "atlas-invoice-q1-final.txt",
      "content": "Atlas Holdings — Invoice 2025-Q1-final\n…\nThis invoice is confidential and should not be shared with audit.",
      "heuristics": [
        { "name": "consistency", "rating": "medium", "description": "Total aligns with one line item in the memo, but issued before the dates referenced there." },
        { "name": "references",  "rating": "low",    "description": "No invoice number scheme matches Atlas Holdings' publicly known billing format." },
        { "name": "red_flags",   "rating": "high",   "description": "Line item literally reads 'off-book' and the document instructs the recipient not to share with audit — strong indicator of intent to conceal." }
      ]
    }
  ]
}
```

`content` is the raw text of the document — render in a monospace/preformatted block to preserve whitespace and newlines. (In the live mock, the same value is returned regardless of what was submitted to `POST /cases`.)

**Errors:**
- `404 case_not_found`, `404 category_not_found`.

---

## Error envelope

All `4xx`/`5xx` responses share this shape:

```json
{ "error": { "code": "case_not_found", "message": "no case with id 99" } }
```

| Code | Status | Meaning |
|------|--------|---------|
| `invalid_request` | 400 | Body is malformed or missing required fields. |
| `case_not_found` | 404 | No case exists with the given ID. |
| `category_not_found` | 404 | The case exists but has no category with that ID. |

---

## Suggested frontend flow

1. **Submit** — user pastes/uploads text documents. `POST /cases`. Capture `case_id` and route to the case overview.
2. **Overview** — `GET /cases/{case_id}`. If `status` is `processing`, show a pending state and poll. Once `complete`, render category cards. Sort client-side by triage (high → medium → low) to draw the eye.
3. **Drill into a category** — `GET /cases/{case_id}/categories/{category_id}` for the category-level signal. You can also fire `GET /cases/{case_id}/categories/{category_id}/documents` in parallel and render documents in a side panel or below the heuristics.
4. **Inspect a document** — within the documents view, expandable cards showing filename, raw content, and per-doc heuristics.

## TypeScript types

```ts
type Rating = "high" | "medium" | "low";

export interface Heuristic {
  name: string;
  rating: Rating;
  description: string;
}

export interface CategorySummary {
  id: number;
  title: string;
  triage: Rating;
  description: string;
}

export type CaseStatus = "processing" | "complete" | "failed";

export interface CaseSummary {
  case_id: number;
  created_at: string; // RFC3339
  status: CaseStatus;
  document_count: number;
  categories: CategorySummary[];
}

export interface CategoryDetailResponse {
  case_id: number;
  category: {
    id: number;
    title: string;
    triage: Rating;
    description: string;
    document_count: number;
    heuristics: Heuristic[];
  };
}

export interface DocumentRecord {
  id: number;
  filename: string;
  content: string;
  heuristics: Heuristic[];
}

export interface CategoryDocumentsResponse {
  case_id: number;
  category_id: number;
  documents: DocumentRecord[];
}

export interface ApiError {
  error: { code: string; message: string };
}

export interface CreateCaseRequest {
  documents: { filename?: string; content: string }[];
}

export interface CreateCaseResponse {
  case_id: number;
}
```
