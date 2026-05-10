# Prompt: per-document heuristic demos

**API behavior demonstrated**: every document is graded on a fixed set of four heuristics:

| Heuristic | Signal | High = |
|-----------|--------|--------|
| `consistency` | positive | internally coherent, no contradictions |
| `references` | positive | concrete, externally checkable references |
| `emotive_language` | negative | inflammatory or emotional rhetoric |
| `ideology` | negative | motivated by agenda or vendetta |

This prompt produces a battery of single-doc tips, each engineered to pin **one** heuristic toward a specific rating while keeping the others neutral. Use these to demo or regression-test the per-document classifier.

For all prompts: read `00-style-guide.md` first.

---

## `consistency` — low

> Generate **one tip** that is plausible on its surface but contains **at least three internal contradictions**. Examples: a date that places an event before the submitter started working at the company; a dollar amount that doesn't match the sum of the line items the submitter lists; a statement that the submitter "personally witnessed" something that they later say happened while they were on parental leave; a named witness who is described both as a junior associate and as the head of the compliance team.
>
> Keep the *style* procedural and the *references* concrete — the goal is to isolate `consistency` as the failing axis. The other three heuristics should be neutral.

## `consistency` — high

> Generate **one tip** describing a multi-step procurement scheme over a 2-year period with **at least seven specific factual claims** (names, dates, amounts, contract numbers) that are all internally consistent: the timeline checks out, dollar amounts add up, the submitter's described tenure aligns with the events they witnessed.

---

## `references` — low

> Generate **one tip** that is calmly written and internally consistent but **has almost no concrete references**: no full names (just "my boss," "a senior person"), no specific dates (just "a couple of years ago"), no dollar amounts, no contract numbers, no organization names beyond a generic "the company." The submitter genuinely seems to have observed wrongdoing but is being so cautious about identifying details that nothing here is externally checkable.

## `references` — high

> Generate **one tip** packed with concrete verifiable references: full names of at least four people with their job titles, exact dates (DD Month YYYY), dollar amounts to the dollar, contract or PO numbers, an address, a named bank or shell company, a named jurisdiction. The submitter offers documents and describes them specifically (e.g., "the November 14, 2024 board minutes, page 7").

---

## `emotive_language` — low

> Generate **one tip** describing serious wrongdoing in **flat, procedural language** — no exclamations, no value-laden adjectives, no descriptions of personal toll or moral framing. The submitter writes like an auditor's memo: subject, what was observed, what evidence exists, what action is requested. Even the most damning details are stated neutrally.

## `emotive_language` — high

> Generate **one tip** that is genuinely about wrongdoing but written by someone who is **visibly furious or distraught**: heavy use of exclamation marks, value-laden adjectives ("disgusting," "evil," "shameful"), repeated emotional asides, descriptions of crying, of sleepless nights, of the "rage I feel every time I think about this." The factual core is real — names, dates, dollar amounts are still in there — but the rhetoric is dominant.
>
> Important: do **not** make it ideological. The anger should be specific to people and events, not a critique of a political system or class.

---

## `ideology` — low

> Generate **one tip** with **zero political or ideological framing**. The submitter is reporting wrongdoing because it's wrongdoing, not because it fits a worldview. No language about "the system," no class or partisan framing, no broader-cause framing. Just: this happened, here's the evidence.

## `ideology` — high

> Generate **one tip** where the wrongdoing is real and the specifics are present, but the submitter's **motivation is foregrounded as ideological**: the submitter explains they are part of a political organization, names the broader cause they are advancing, frames the wrongdoing as a symptom of a larger ideological problem (capitalism, deep state, foreign influence, partisan corruption, religious decay — pick whichever fits the case). The political framing is in the first paragraph and recurs throughout.
>
> Important: do **not** make it emotive. The submitter should be calm and articulate. The agenda should come through clearly, but coolly.

---

## How to verify

Each generated tip can be submitted as a single-document case. For each tip, `GET /cases/{id}/topics/{tid}/documents` should return the targeted heuristic at the expected rating, with descriptions that line up with the prompt's intent.

Use this set as a regression check: any prompt change to the per-document heuristic system prompts should still produce roughly the same ratings on this battery.
