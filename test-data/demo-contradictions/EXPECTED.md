# Expected: Demo 2 — Internal Contradictions

Purpose: pitch demo. Demonstrates that the system reads each document
critically rather than accepting its claims at face value, and that it
can also recognize when an apparent inconsistency is explained inside
the document itself (false-positive resistance).

## Per-document expected behavior

### `tip1.txt` — Pier 7 auto loan APR (DIRTY)

`consistency` should rate **low**.

Seeded inconsistencies:

- Temporal: source says "I joined the bank in March 2023 and have been on
  the consumer compliance team for the entire period since" but also says
  "I personally attended the rate-setting meeting in November 2022 where
  Mendin presented the 'rate floor adjustment' framework." A March 2023
  joiner cannot have been at a November 2022 internal meeting.
- Numerical: source claims "4,200 affected loans averaging $450 in
  additional interest charges per loan, totaling approximately $2.1
  million." 4,200 × $450 = $1.89 million, not $2.1 million. The headline
  total does not reconcile against the stated components.

The system should flag at least one of these. Other heuristics (`references`)
should still rate medium / high — the source has documents, dates, and named
individuals, so this is a doc with strong references but weak internal
consistency.

### `tip2.txt` — Cordwain Logistics data exposure (DIRTY)

`consistency` should rate **low**.

Seeded inconsistencies:

- Role / access: source identifies as a "senior network engineer" but
  later writes "I had read access during this period to the CFO's
  confidential exposure analysis prepared for the board." A network
  engineer is not a typical reader of CFO board-level financial analyses.
- Identity drift: the CISO is named "Rohan Bakshi" in paragraph 3, "Rohan
  Bashki" in paragraph 5 in the context of signing the board package, and
  "Bakshi" again at the close. The same person's surname is spelled two
  different ways within the document.
- Bonus tell (the system may or may not surface): the phrase "the bank —
  sorry, the company" mid-paragraph reads as a copy-paste artifact from
  another draft.

The system should flag at least one of these. As with tip1, `references`
should still rate respectably; the contradiction signal should not crater
all heuristics.

### `tip3.txt` — Belden Regional CRNA diversion (CLEAN)

`consistency` should rate **medium / high**.

This document contains three apparent inconsistencies that all resolve on
careful reading. The system should NOT flag it as inconsistent.

Apparent issues that resolve:

- "CRNA" vs "nurse" — the source establishes in paragraph 2 that they
  will use the two terms in different registers, citing the professional
  code basis for the nurse framing.
- "Fiscal Q2 2025" vs "calendar Q4 2024" — the source explicitly defines
  the fiscal-to-calendar mapping in the same sentence the fiscal period
  is introduced.
- "27 vials" vs "29 vials" — the source explicitly distinguishes between
  vials confirmed missing at fiscal Q2 close (27) and total suspected
  diversions including Q3-identified items originating from Q2
  dispensings (29).

This is the false-positive control. A naive consistency detector that
matches surface patterns will fire on this document. A reading detector
should not.

## Case-level checks

- All three documents should land under one or two semantically related
  topics; topic count is not the focus of this demo.
- Per-doc heuristics returned by `/topics/{tid}/documents` should show
  the consistency-low / consistency-low / consistency-acceptable pattern
  described above.

## Pitch beats

- Show all three submissions side by side.
- Open tip1; point at the consistency flag and the system's explanation
  (the temporal and / or numerical issue).
- Open tip2; point at the consistency flag and the role/access or
  identity-drift issue.
- Open tip3; show that this looked like it should be flagged on a quick
  read but the system did not flag it. Walk through one of the apparent
  issues (the fiscal-calendar mapping or the vial count) and explain
  that the document explained itself.
- Close with: "this isn't pattern-matching — the system reads."
