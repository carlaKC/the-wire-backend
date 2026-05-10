# Prompt: topic-grouping demo

**API behavior demonstrated**: case classification clusters documents by topic. A single dump containing tips on several distinct subjects should produce several distinct topics, with multiple tips landing in the same topic when they're about the same subject.

## Use this prompt

> Read `00-style-guide.md` first; tips must follow that format.
>
> Generate **8 whistleblower tips** for a single document dump. They should cluster into **three topics** as follows:
>
> 1. **Topic A — Public-sector procurement fraud** (3 tips, from 3 different submitters at unrelated agencies):
>    - Tip A1: a contracts analyst at a state transportation department describing bid-steering toward one favored contractor.
>    - Tip A2: a procurement officer at a municipal utility describing rigged technical scoring after RFP close.
>    - Tip A3: a former employee of a defense subcontractor describing kickbacks routed through a consulting firm.
>    - These tips share a topic but **must not** describe the same agency, the same contractor, or the same people. Different jurisdictions, different industries within "public-sector procurement."
>
> 2. **Topic B — Pharmaceutical safety data manipulation** (2 tips):
>    - Tip B1: a clinical data analyst describing reclassification of adverse events before an FDA submission.
>    - Tip B2: a CRO statistician describing exclusion of trial sites that ran high on a key adverse-event endpoint.
>    - Different drugs, different companies, different drug classes. Both about pre-submission data hygiene.
>
> 3. **Topic C — Workplace harassment cover-up** (3 tips):
>    - Tip C1: an associate at a consulting firm reporting a senior partner; HR consulted with legal and the partner was promoted.
>    - Tip C2: a researcher at a university lab reporting a PI; Title IX office sat on the complaint.
>    - Tip C3: a flight attendant reporting a chief pilot; airline HR moved the complainant to a different base.
>    - Different industries, different power dynamics, but all share the pattern of internal report → no real action.
>
> The eight tips should be **dropped into a single dump in random order**. The classifier should be able to recover the three topics regardless of order.
>
> Each tip must include the named-people / dates / dollar-amounts level of specificity from the style guide so that per-document heuristics still have something to grade.

## How to verify

After running this case through the API:

- `GET /cases/{id}` should return three topics with `document_count` of 3, 2, and 3.
- The three Topic A tips should not bleed into other topics.
- Submitting these tips a second time as a new case should reuse the same global topic IDs (the API reuses topics across cases).
