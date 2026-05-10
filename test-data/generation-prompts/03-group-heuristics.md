# Prompt: group-heuristic demos

**API behavior demonstrated**: after per-document filtering, the API runs a group scan over the unfiltered docs in each topic. The scan attaches topic-level heuristics like `corroboration`, `shared_references`, `coordinated_framing`, `shared_agenda`, `contested_narrative`, `timeline_coherence`, and `temporal_scope`.

This file contains **one prompt per group heuristic**. Each prompt produces a multi-document set targeting that one heuristic. To exercise multiple group heuristics in one case, you can mix sets together — but it's clearer for demonstration to keep each set as its own case.

For all prompts: read `00-style-guide.md` first.

---

## A. `corroboration` (positive) — high

> Generate **three whistleblower tips** about the same alleged wrongdoing — **a sustained pattern of revenue pulled forward at a publicly traded SaaS company called Pelham Analytics through unusual quarter-end "early start" contract clauses** — but from three different submitters with three different vantage points:
>
> 1. A senior revenue accountant on the controller's team who sees the contracts after they're signed.
> 2. A deal desk analyst who sees the negotiation and signing pressure in the last week of the quarter.
> 3. A former enterprise account executive who left in protest after being told to "make the math work" on a specific deal.
>
> Each tip should independently describe **overlapping but not identical** specifics: the same company, the same general practice, same approximate timeframe (Q1 2025 onward). Each submitter knows things the others don't: the accountant has the contract spreadsheet, the deal-desk analyst has the workflow logs, the AE has the email instructions.
>
> Critically: **do not coordinate the language**. Each submitter writes in their own voice. The factual overlap should come from describing real shared events, not from shared phrasing.

**Expected**: high `corroboration`, high `shared_references`, low `coordinated_framing`.

---

## B. `shared_references` (positive) — high

> Generate **three tips** that each, independently, name the same small set of concrete artifacts: a specific Cyprus-registered shell company called **Kavala Trading Limited**, a specific intermediary named **Ramzi Atwan**, and a specific procurement officer named **Bashar Hijazi**. Each tip describes a *different* contract or transaction routed through this triangle, from a different submitter (one auditor, one former bookkeeper, one foreign procurement counterparty). Different facts; same names.

**Expected**: high `shared_references`, also likely high `corroboration`.

---

## C. `coordinated_framing` (negative) — high

> Generate **four tips** that all complain about a non-profit called **Bayfront Outreach** and all use suspiciously similar language: each tip independently uses the phrases "deeply troubling," "serial mismanagement," and "the donors deserve answers." Each tip is signed by a different alleged former volunteer. The specifics are vague — small dollar amounts, no named events, no documents offered. The submissions arrive within roughly the same week.
>
> The tone should feel like a coordinated campaign rather than independent reporting: shared phrases, shared catchphrases for the executive director, shared framing about "the donors."

**Expected**: high `coordinated_framing`. Per-doc `references` will likely be low; `emotive_language` may be medium. Some or all may be filtered, in which case the group scan is skipped — that's also a demonstrable outcome. If you want the group scan to run, soften the emotive language slightly so at least two docs remain unfiltered.

---

## D. `shared_agenda` (negative) — high

> Generate **three tips** that all target the same school district superintendent, **Dr. Elaine Voss**. Each tip is from a "concerned parent" at a different school in the district. Each tip raises a *different* concrete-sounding allegation (budgetary, hiring, curriculum), but each tip also wraps the allegation inside a broader political agenda: each submitter is open about being a member of the same advocacy group that has been campaigning to remove Voss for unrelated reasons. The political framing is foregrounded; the wrongdoing claims are vague on dates, amounts, and witnesses.

**Expected**: high `shared_agenda`. Per-doc `ideology` will be high, so all three may be filtered and the group scan would be skipped. To demonstrate the group heuristic explicitly, include at least one fourth tip that is more procedural and lower on `ideology`, so it survives filtering and the group scan runs over a small set.

---

## E. `contested_narrative` (positive) — high

> Generate **two tips** describing **the same incident** — a workplace altercation at a venture-capital firm called **North Reach Capital** between a senior partner, **Ben Coltrane**, and an associate, **Tessa Aleshire** — from two opposing perspectives:
>
> - Tip 1 (from a colleague who was present): describes Coltrane verbally berating Aleshire and physically blocking her from leaving the conference room.
> - Tip 2 (from a different colleague who was present): describes Aleshire as the aggressor, recording the encounter for HR purposes, and Coltrane as having been entrapped.
>
> Both writers claim to have been in the room. Each names the others as witnesses. The accounts conflict on motive and on who escalated.

**Expected**: high `contested_narrative`. This does **not** affect filtering — both docs should pass. The signal is for journalist review.

---

## F. `timeline_coherence` (positive) — high

> Generate **three tips** that, between them, form a coherent timeline of an environmental incident at a chemical plant called **Trenholm Plating Works**:
>
> 1. A line technician describes seeing rinse water being discharged on weekend nights from late 2021 through 2024.
> 2. A delivery driver for the licensed treatment vendor describes a documented drop in tankered waste pickups beginning in early 2022 and continuing through late 2024.
> 3. A municipal water-quality tester describes anomalous chromium readings downstream of the plant beginning in mid-2022, peaking in 2023, declining slightly after a state inspection in 2024.
>
> The dates across the three tips should be **consistent and mutually reinforcing**: the discharge period, the tanker drop, and the chromium readings should all overlap.

**Expected**: high `timeline_coherence`, also high `corroboration` and `temporal_scope` (sustained).

---

## G. `temporal_scope` — sustained

> Generate **two tips** about ongoing kickbacks between a state DOT supervisor and a paving contractor, deliberately framed to make the conduct appear **sustained over multiple years (2019–2025)** rather than isolated to a single quarter or single contract. Each tip should reference the long-running nature: monthly handoffs, repeated annual contract renewals, multiple successor accountants who saw the same pattern.

**Expected**: `temporal_scope` rated to reflect sustained recurring conduct (not isolated). Pair with the `01-topic-grouping.md` set for contrast: those tips describe shorter, more isolated incidents.

---

## How to verify

For each set above, after running it as a case:

- `GET /cases/{id}/topics/{tid}` should return the targeted heuristic in the `heuristics` array with the expected rating.
- Verify that filtered documents (if any) are excluded from the group scan input by checking that the group heuristics' descriptions reference only the unfiltered documents.
