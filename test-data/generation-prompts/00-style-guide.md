# Style guide for generated tips

Every prompt in this directory references this style guide. Paste it ahead of (or together with) the prompt you're using.

## What a tip looks like

Each tip is a single plain-text file representing one whistleblower submission. They are first-person accounts written by people who believe they have observed wrongdoing and want a journalist or investigator to look into it.

## Header

Every tip begins with three header lines:

```
TIP <N>: <one-line subject>
CHANNEL: <intake form | secure drop | encrypted email>
RECEIVED: <DD Month YYYY>   (optional; include for ~half of tips)
```

Then a blank line, then the body.

## Body

- 250–500 words, occasionally longer.
- First-person ("I worked at…", "I was in the room when…").
- The submitter has a real role: an employee, a contractor, an observer. State the role, the dates of employment / observation, and how they came to know what they're describing.
- Include concrete, externally-verifiable facts: named people (full names), named organizations, dates (month + year minimum), dollar amounts, contract numbers, addresses, named events. These are what the model later extracts as `facts_to_verify`, so they need to be specific.
- Include at least one named secondary actor (a colleague, a supervisor, an HR contact) — these tips usually implicate more than one person.
- The submitter usually has documents, recordings, or notes. Say what they have and how they would share it.
- Tone is plain and procedural by default. Submitters are nervous, but most are trying to be credible.

## What to vary across a set

When generating multiple tips that will be processed together as one case, deliberately vary:

- **Topic** — procurement, environmental, accounting, harassment, safety, public-sector kickbacks, regulatory fraud, etc.
- **Industry** — pharma, construction, plating, IoT/SaaS, municipal government, NGO, defense.
- **Submission channel** — secure drop, intake form, encrypted email.
- **Voice** — calm and meticulous; nervous; angry; weary; legalistic. Each submitter is a different person.
- **Document strength** — some have hard evidence (contracts, recordings); some only have what they observed.

## What to avoid (unless the prompt explicitly asks for them)

- Repeated phrases or tone across tips.
- Vague accusations with no names, dates, or amounts.
- Editorial framing ("This is the worst thing I've ever seen") unless you're being asked to demonstrate emotive language.
- Political or ideological framing ("typical of how this party operates") unless you're being asked to demonstrate the ideology heuristic.
- Real living people, real companies. Use plausible but invented names.

## Output

Produce each tip as raw text (not JSON). The user will assemble them into the API's `case-test.json` shape afterward — see `99-assembling-case-test-json.md`.
