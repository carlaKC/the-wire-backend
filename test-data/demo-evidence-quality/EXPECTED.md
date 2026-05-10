# Expected: Demo 4 — Evidence Quality

Purpose: pitch demo. Two tips submitted independently about the same
allegation against the same target. One contains the kind of specific,
verifiable detail an investigator can act on; the other is rumor without
documentation. The system should grade these very differently on
per-document evidence heuristics, even though both describe the same
underlying issue.

## Documents

- `tip1.txt` — first-person source (CFO's executive assistant) reporting
  CFO Andriy Kovalenko of Heronforge Industrial misusing the corporate
  card. Provides six specific transactions with dates, dollar amounts,
  vendor names, folio / invoice numbers, named family members, and
  describes retained documentation.
- `tip2.txt` — anonymous former employee reporting the same allegation
  against the same person. No specific dates, no specific amounts, no
  documents, sourced to "a friend in finance" and "people."

## Topic-level expectation

Both tips should land in the same topic — they describe the same
allegation against the same individual at the same company. The system
should not split them.

## Per-document heuristic expectation

### `tip1.txt` (HIGH evidence quality)

- `references` should rate **high**. The document names dates, dollar
  amounts, vendor names, hotel folio numbers, invoice numbers, named
  family members, the aircraft tail and route, and the coding categories
  used to disguise expenses. The source describes specific retained
  documents and is willing to provide them.
- `consistency` should rate **high / medium**. The document is
  internally coherent — the six transactions sum approximately to the
  $51,000 figure cited.
- `emotive_language` should rate **low / medium**. The voice is
  procedural, near-clinical. There is one mild characterization
  ("instructed me on multiple specific occasions"). No invective.
- `ideology` should rate **low**. The document is not framed in
  ideological terms; it is a descriptive enumeration of transactions.

### `tip2.txt` (LOW evidence quality)

- `references` should rate **low**. The document contains no specific
  dates, no specific amounts, no folio or invoice numbers, no named
  documents, and explicitly states "I don't have documents." Sources are
  attributed to "a friend in finance," "people have told me," and "I
  heard." The single specific recurring element is the target's name.
- `consistency` should rate **medium / acceptable**. There are no
  internal contradictions; the document is just thin. (Do not penalize
  consistency for thinness.)
- `emotive_language` should rate **medium**. Phrases like "treating the
  company like his personal piggybank" and "it's not right" register as
  characterization rather than description.
- `ideology` should rate **low / medium**. The document gestures at a
  shareholder-rights frame at the close ("investors should know") that
  pushes the rating slightly above pure descriptive but does not crater.

The contrast between the two `references` ratings is the key signal for
the demo.

## Pitch beats

- Open the case detail. Show two documents in one topic against the
  same target.
- Open `tip1.txt`. Show the heuristic ratings panel — references high,
  consistency high, evidence concrete and specific.
- Open `tip2.txt`. Show the heuristic ratings panel — references low.
- Read aloud one specific transaction from tip1 (the Aspen flight, the
  anniversary purchase) and one passage from tip2 ("I heard from a
  friend who is still in finance there").
- Make the point: same allegation, very different reportability. The
  system can tell the difference.
