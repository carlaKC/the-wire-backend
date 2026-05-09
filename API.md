# The Wire — Document Grading API

HTTP API for submitting whistleblower document dumps and retrieving topic-grouped, heuristic-graded results. The server classifies submitted text through a Maple-compatible chat completion endpoint and stores processed cases in memory.

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

# 2. List topics
curl http://localhost:8080/api/v1/cases/1

# 3. Topic detail (heuristics for the topic as a whole)
curl http://localhost:8080/api/v1/cases/1/topics/1

# 4. Documents in a topic (raw content + per-document heuristics)
curl http://localhost:8080/api/v1/cases/1/topics/1/documents
```

## Concepts

- **Case** — one document dump. Created by POSTing a set of text documents. Identified by an integer.
- **Topic** — a reusable grouping the service infers from submitted documents (e.g. "Financial irregularities"). Topic IDs are global within the running server process, so documents in a new case can be assigned to a topic created by an older case. Case endpoints return only topics represented in that case.
- **Document** — one text file from the original submission. Each document belongs to exactly one topic.
- **Heuristic** — a named graded signal: `{ name, signal, rating, description }`. Appears at the **topic level** (signal across the whole topic) and the **document level** (signal for that one document). Topic-level heuristics are an **open** set; document-level heuristics are a **closed** set of four (see [Document heuristics](#document-heuristics)).

## Enumerations

- **Triage** (topic-level): `"high" | "medium" | "low"`. Use for visual emphasis — high = alert, medium = caution, low = informational.
- **Heuristic rating**: `"high" | "medium" | "low"`. Polarity depends on the heuristic's `signal` (below).
- **Heuristic signal**: `"positive" | "negative" | ""`. Tells the client whether a high `rating` is favorable or unfavorable for the submission. `positive` → high is good (e.g. high `consistency` is desirable). `negative` → high is bad (e.g. high `emotive_language` is concerning). Empty string means no directionality is asserted — render the rating neutrally and rely on the `description`. Topic-level heuristics currently always have an empty `signal`.

## Document heuristics

The `/cases/{case_id}/topics/{topic_id}/documents` endpoint always returns this fixed set of four heuristics per document, sourced from a dedicated per-document analysis pass (separate from the topic-classification pass):

| Name | Signal | What it means |
|------|--------|---------------|
| `consistency` | positive | Document is internally coherent and free from obvious factual contradictions or impossible timelines. |
| `references` | positive | Document includes concrete, potentially verifiable references (transaction IDs, dates, addresses, named events, etc.). |
| `emotive_language` | negative | Document relies on emotional rhetoric, inflammatory wording, or personal attacks rather than factual descriptions. |
| `ideology_or_incentives` | negative | Document appears motivated by ideological persuasion, agenda, vendetta, or financial incentive rather than reporting. |

The names and signals are stable. The `rating` and `description` are produced per document by the model.

## Processing model

`POST /cases` returns the case ID immediately. Analysis runs asynchronously, and the case progresses through `status` values on the case summary:

| Status | Meaning |
|--------|---------|
| `processing` | Analysis is in flight. `topics` may be empty or partial. Per-document `heuristics` may be empty (the per-document analysis pass runs concurrently with topic classification and may not have produced output yet). Topic endpoints (`/topics/{tid}` and `/topics/{tid}/documents`) return `404 topic_not_found` until the topic is materialized. |
| `complete` | Analysis finished. All data populated. |
| `failed` | Analysis errored. The case is terminal; further polling will not change the result. |

Clients should poll `GET /cases/{id}` until `status` is `complete` or `failed`.

The current mock implementation is effectively synchronous: every newly created case is reported with `status: "complete"` immediately.

Immediately after creation, `GET /cases/{case_id}` can return the normal case summary shape with an empty `topics` array. Topic and document endpoints return `topic_not_found` until classification has produced topics. Once classification completes, the same GET endpoints return final data.

Cases are stored in process memory. Restarting the server clears previously created cases.

## Classification mapping

- `POST /cases` validates the request, returns the case id, and sends submitted document text to Maple in the background.
- The server sends existing global topics to Maple. Maple assigns each document to an existing topic when one fits, or returns a new topic title/description when no existing topic fits.
- Topic IDs are assigned by the server and are reusable across cases.
- The original submitted filename/content is preserved in document responses.
- Topic `triage` on case endpoints is derived from the highest sensitivity seen for that topic in that case: level 1 -> `low`, level 2 -> `medium`, levels 3-4 -> `high`.
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

Case summary — the topics the service inferred for this dump.

**Response 200**:
```json
{
  "case_id": 1,
  "created_at": "2026-05-09T12:34:56Z",
  "status": "complete",
  "document_count": 4,
  "topics": [
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
| `document_count` | integer | Total documents in the case (sum across topics). |
| `topics` | array | Case-scoped list of global topic IDs represented in this case. Order is service-defined; sort on the client if you need triage-first ordering. May be empty while `status` is `processing`. |

**Errors:**
- `404 case_not_found`.

---

### GET `/cases/{case_id}/topics/{topic_id}`

Topic detail with topic-level heuristics for this case. The `topic.id` is a global topic ID and may appear in other cases.

**Response 200**:
```json
{
  "case_id": 1,
  "topic": {
    "id": 1,
    "title": "Procurement",
    "triage": "high",
    "description": "The memo discusses vendor approval gaps.",
    "document_count": 1,
    "heuristics": [
      { "name": "sensitivity", "signal": "", "rating": "high", "description": "Highest sensitivity level in this topic is 3." },
      { "name": "claims", "signal": "", "rating": "medium", "description": "1 factual claim(s) extracted in this topic." }
    ]
  }
}
```

`heuristics` is an open list — render every entry the server returns. Don't assume a fixed length or fixed names. Topic-level heuristics currently emit an empty `signal`; render their `rating` neutrally.

**Errors:**
- `404 case_not_found`, `404 topic_not_found`.

---

### GET `/cases/{case_id}/topics/{topic_id}/documents`

The documents from this case that are assigned to the topic, with their raw content and per-document heuristics. Even when the topic ID exists in other cases, this endpoint returns only documents from `{case_id}`.

Each document carries the closed set of four per-document heuristics described in [Document heuristics](#document-heuristics). The `heuristics` array may be empty if the per-document analysis pass produced nothing for that document (e.g. while the case is still `processing`).

**Response 200**:
```json
{
  "case_id": 1,
  "topic_id": 1,
  "documents": [
    {
      "id": 1,
      "filename": "memo.txt",
      "content": "INTERNAL MEMO\n\nDate: 2025-04-15\nFrom: J. Doe, Finance\nTo:   M. Smith, Procurement\n\nRe: Q2 off-cycle vendor disbursements\n…",
      "heuristics": [
        { "name": "consistency",            "signal": "positive", "rating": "high",   "description": "Dates, parties, and dollar amounts are internally consistent." },
        { "name": "references",             "signal": "positive", "rating": "medium", "description": "Names a vendor and an invoice number; no transaction IDs." },
        { "name": "emotive_language",       "signal": "negative", "rating": "low",    "description": "Tone is procedural; no inflammatory language." },
        { "name": "ideology_or_incentives", "signal": "negative", "rating": "low",    "description": "No agenda or personal incentives are evident." }
      ]
    }
  ]
}
```

`content` is the raw text of the document — render in a monospace/preformatted block to preserve whitespace and newlines.

**Errors:**
- `404 case_not_found`, `404 topic_not_found`.

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
| `topic_not_found` | 404 | The case exists but has no topic with that ID. |

---

## Suggested frontend flow

1. **Submit** — user pastes/uploads text documents. `POST /cases`. Capture `case_id` and route to the case overview.
2. **Overview** — `GET /cases/{case_id}`. If `status` is `processing`, show a pending state and poll. Once `complete`, render topic cards. Sort client-side by triage (high → medium → low) to draw the eye.
3. **Drill into a topic** — `GET /cases/{case_id}/topics/{topic_id}` for the topic-level signal. You can also fire `GET /cases/{case_id}/topics/{topic_id}/documents` in parallel and render documents in a side panel or below the heuristics.
4. **Inspect a document** — within the documents view, expandable cards showing filename, raw content, and per-doc heuristics.

## TypeScript types

```ts
type Rating = "high" | "medium" | "low";
type Signal = "positive" | "negative" | "";

export interface Heuristic {
  name: string;
  signal: Signal;
  rating: Rating;
  description: string;
}

export interface TopicSummary {
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
  topics: TopicSummary[];
}

export interface TopicDetailResponse {
  case_id: number;
  topic: {
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

export interface TopicDocumentsResponse {
  case_id: number;
  topic_id: number;
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
