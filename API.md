# The Wire — Document Grading API

HTTP API for submitting whistleblower document dumps and retrieving categorized, heuristic-graded results. The server classifies submitted text through a Maple-compatible chat completion endpoint and stores processed cases in memory.

## Running the server

```sh
export MAPLE_API_KEY=...
go run . --nomock
```

Listens on `:8080` by default (override with `PORT`).

Classification settings:

- `MAPLE_API_KEY` is required when creating cases.
- `MAPLE_BASE_URL` defaults to `http://127.0.0.1:8081`.
- `MAPLE_MODEL` defaults to `deepseek-v4-pro`.

Without `--nomock`, the server keeps the hardcoded mock responses for frontend development. Maple is the only non-mock processing mode.

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
- **Category** — a reusable grouping the service infers from submitted documents (e.g. "Financial irregularities"). Category IDs are global within the running server process, so documents in a new case can be assigned to a category created by an older case. Case endpoints return only categories represented in that case.
- **Document** — one text file from the original submission. Each document belongs to exactly one category.
- **Heuristic** — a named graded signal: `{ name, rating, description }`. Appears at the **category level** (signal across the whole category) and the **document level** (signal for that one document). The set of heuristic names is **open**: render whatever the API returns rather than hardcoding the list.

## Enumerations

- **Triage** (category-level): `"high" | "medium" | "low"`. Use for visual emphasis — high = alert, medium = caution, low = informational.
- **Heuristic rating**: `"high" | "medium" | "low"`. Polarity is heuristic-specific; for unknown heuristic names, render the rating neutrally and rely on the `description`.

## Processing model

`POST /cases` returns the case ID immediately. Analysis runs asynchronously, and the case progresses through `status` values on the case summary:

| Status | Meaning |
|--------|---------|
| `processing` | Analysis is in flight. `categories` may be empty or partial. Per-document `heuristics` may be empty. Category endpoints (`/categories/{cid}` and `/categories/{cid}/documents`) return `404 category_not_found` until the category is materialized. |
| `complete` | Analysis finished. All data populated. |
| `failed` | Analysis errored. The case is terminal; further polling will not change the result. |

Clients should poll `GET /cases/{id}` until `status` is `complete` or `failed`.

The current mock implementation is effectively synchronous: every newly created case is reported with `status: "complete"` immediately.

Immediately after creation, `GET /cases/{case_id}` can return the normal case summary shape with an empty `categories` array. Category and document endpoints return `category_not_found` until classification has produced categories. Once classification completes, the same GET endpoints return final data.

Cases are stored in process memory. Restarting the server clears previously created cases.

## Classification mapping

- `POST /cases` validates the request, returns the case id, and sends submitted document text to Maple in the background.
- The server sends existing global categories to Maple. Maple assigns each document to an existing category when one fits, or returns a new category title/description when no existing category fits.
- Category IDs are assigned by the server and are reusable across cases.
- The original submitted filename/content is preserved in document responses.
- Category `triage` on case endpoints is derived from the highest sensitivity seen for that category in that case: level 1 -> `low`, level 2 -> `medium`, levels 3-4 -> `high`.
- Document IDs are assigned by the server for each case.

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
| `documents[].filename` | string | no | Optional. Returned later with the document. |

**Response 201:**
```json
{ "case_id": 1 }
```

**Errors:**
- `400 invalid_request` — body is not valid JSON, `documents` is missing/empty, or any entry has empty `content`.

---

### GET `/cases/{case_id}`

Case summary — the categories the service inferred for this dump.

**Response 200**:
```json
{
  "case_id": 1,
  "created_at": "2026-05-09T12:34:56Z",
  "status": "complete",
  "document_count": 4,
  "categories": [
    {
      "id": 1,
      "title": "Procurement",
      "triage": "high",
      "description": "The memo discusses vendor approval gaps."
    },
    {
      "id": 2,
      "title": "Communications Messaging",
      "triage": "medium",
      "description": "The email discusses moving conversations off email."
    }
  ]
}
```

| Field | Type | Notes |
|-------|------|-------|
| `created_at` | string | RFC3339 UTC. |
| `status` | string | One of `processing`, `complete`, `failed`. See [Processing model](#processing-model). |
| `document_count` | integer | Total documents in the case (sum across categories). |
| `categories` | array | Case-scoped list of global category IDs represented in this case. Order is service-defined; sort on the client if you need triage-first ordering. May be empty while `status` is `processing`. |

**Errors:**
- `404 case_not_found`.

---

### GET `/cases/{case_id}/categories/{category_id}`

Category detail with category-level heuristics for this case. The `category.id` is a global category ID and may appear in other cases.

**Response 200**:
```json
{
  "case_id": 1,
  "category": {
    "id": 1,
    "title": "Procurement",
    "triage": "high",
    "description": "The memo discusses vendor approval gaps.",
    "document_count": 1,
    "heuristics": [
      { "name": "sensitivity", "rating": "high", "description": "Highest sensitivity level in this category is 3." },
      { "name": "claims", "rating": "medium", "description": "1 factual claim(s) extracted in this category." }
    ]
  }
}
```

`heuristics` is an open list — render every entry the server returns. Don't assume a fixed length or fixed names.

**Errors:**
- `404 case_not_found`, `404 category_not_found`.

---

### GET `/cases/{case_id}/categories/{category_id}/documents`

The documents from this case that are assigned to the category, with their raw content and per-document heuristics. Even when the category ID exists in other cases, this endpoint returns only documents from `{case_id}`.

**Response 200**:
```json
{
  "case_id": 1,
  "category_id": 1,
  "documents": [
    {
      "id": 1,
      "filename": "memo.txt",
      "content": "INTERNAL MEMO\n\nDate: 2025-04-15\nFrom: J. Doe, Finance\nTo:   M. Smith, Procurement\n\nRe: Q2 off-cycle vendor disbursements\n…",
      "heuristics": [
        { "name": "classification_rationale", "rating": "high", "description": "The memo discusses vendor approval gaps." },
        { "name": "claim_supported", "rating": "high", "description": "Atlas lacked a purchase order. Evidence: no PO on file" }
      ]
    }
  ]
}
```

`content` is the raw text of the document — render in a monospace/preformatted block to preserve whitespace and newlines.

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
