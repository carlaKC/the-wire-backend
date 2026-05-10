# Prompt: per-document filtering demo

**API behavior demonstrated**: a document is `filtered: true` only when `emotive_language` is `high` and `ideology` is `medium` or `high`. If `emotive_language` is below `high`, or `ideology` is `low`, the document passes filtering. Filtered documents are excluded from the group-heuristic scan.

This prompt produces a four-tip set that hits each cell of the 2×2:

|                          | emotive low/medium | emotive high      |
|--------------------------|--------------------|-------------------|
| **ideology low**         | passes (T1)        | passes (T2)       |
| **ideology medium/high** | passes (T3)        | **filtered (T4)** |

## Use this prompt

> Read `00-style-guide.md` first; tips must follow that format.
>
> Generate **four whistleblower tips** that all describe the same broad subject — **wage theft and unpaid overtime at a regional logistics company** — but vary on the two negative-signal heuristics:
>
> **Tip T1 — emotive low, ideology low (control: should pass filtering).**
> A senior payroll analyst describes, in calm procedural language, a documented pattern of overtime hours being clipped from punch-card data before pay runs. Names the payroll director, dates, the specific software system, the dollar magnitude across a quarter, and the existence of audit logs. No editorializing about the workers, the company, or "how this country treats workers." Just the mechanics.
>
> **Tip T2 — emotive high, ideology low (passes filtering: only one negative is high).**
> A floor supervisor describes the same kind of wage theft they personally witnessed. The factual core is real (named manager, named software, dates, observed shorting of hours). But the writing is angry, raw, and visibly emotional — repeated exclamations, descriptions of the personal toll, "these are people with kids," "I cannot sleep at night." No political framing, no "this is what corporations do" — the anger is about specific people and specific harms.
>
> **Tip T3 — emotive low, ideology high (passes filtering: only one negative is high).**
> A former HR generalist writes calmly, but the framing is ideological. The substance includes some real specifics (a manager, a payroll system, a date range) but they're scaffolded inside a broader argument that wage theft is a deliberate feature of late-stage capitalism, that the company's leadership is morally bankrupt, and that this tip is part of a broader political project the writer is engaged in. The reporting is genuine, but the agenda is foregrounded.
>
> **Tip T4 — emotive high, ideology high (filtered).**
> A submitter who is both furious and ideologically driven. Heavy emotional language ("evil," "criminal," "stolen from us"), repeated political framing about class warfare or anti-worker conspiracy, name-calling of named executives ("greedy parasites"). Some real specifics survive (a name, a date) but they're buried under rhetoric and generalized accusations against a broader class of people. **This tip should not survive negative-heuristic filtering.**
>
> Each tip is from a different submitter at the same company; they are roughly contemporaneous; they describe the same underlying conduct from different vantage points.

## How to verify

After running this case:

- `GET /cases/{id}/topics/{tid}/documents` for the wage-theft topic should return all four documents with `filtered` set per the table above.
- T4 should be **excluded** from the group-heuristic scan. With three remaining unfiltered documents, the topic detail should still include group heuristics like `corroboration` and `shared_references`.
- For each doc, render the four heuristics and check the model's reasoning lines up with the prompt's intent.
