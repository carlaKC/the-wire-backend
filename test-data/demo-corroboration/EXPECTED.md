# Expected: Demo 3 — Corroboration

Purpose: pitch demo. Three independent tips from three different vantage
points all describe the same underlying scheme — Lattimer Medical paying
inflated speaker / consulting / research fees to interventional
cardiologists at Mercer Heart Institute in exchange for institutional
purchasing preference. The system should recognize that all three
documents belong to the same topic.

## Documents

- `tip1.txt` — Lattimer Medical compliance officer (insider at the
  manufacturer). Sees the payment patterns, the lack of corresponding
  speaker / consulting deliverables, and the research-grant burn without
  enrollment.
- `tip2.txt` — Vorian Medical sales account executive (competitor).
  Sees the Mercer contract decision overriding favorable price and
  outcomes data, and quotes a Mercer purchasing director saying the
  decision is driven by "clinical relationship" rather than data.
- `tip3.txt` — Cardiology fellow at Mercer Heart Institute (junior
  insider at the buying institution). Observes the device-selection
  pattern, sees the Open Payments disclosures on the cath lab break-room
  pinboard, and describes the speaker dinners as substantively
  ceremonial.

## Case-level checks

- All three tips should be assigned to the **same topic**.
- The topic title and description should reflect that this is a single
  scheme observed from multiple vantage points (e.g., "Medical device
  speaker / consulting payments tied to institutional purchasing
  preference at Mercer Heart Institute" or similar). It must not split
  the documents into three separate topics.
- The topic's importance should rate `high` — kickback / Anti-Kickback
  Statute exposure on a federal-payor product is a high-importance
  category.

## Cross-document signals the system should pick up

These named overlaps are the corroboration anchors:

- All three tips name **Lattimer Medical** as the manufacturer.
- All three tips name **Mercer Heart Institute** as the institution.
- Tips 1 and 3 both name **Dr. R. Vehring** as a paid Mercer
  interventional cardiologist.
- Tips 1 and 3 both reference **eleven** named-or-disclosed paid Mercer
  interventional cardiologists at high payment volumes, derivable from
  Open Payments data.
- Tip 1 describes the speaker dinners from the audit side; tip 3
  describes attending the same dinners as a fellow. The descriptions
  match.
- Tip 2's "clinical relationship that this institution is not going to
  walk away from" quote from the Mercer purchasing director is the
  external-facing version of the internal pattern tips 1 and 3
  describe.

## Pitch beats

- Open the case detail. Show one topic with three documents from three
  visibly different sources (a manufacturer compliance officer, a
  competitor sales rep, a teaching-hospital fellow).
- Click into the topic description and read aloud the line that
  consolidates the underlying scheme.
- Walk through one anchor — the system identified that all three
  documents name Lattimer + Mercer + the same physician (Vehring) and
  describe overlapping mechanisms.
- Close with: "one tip is a complaint. Three independent tips
  describing the same scheme is a story. The system found the story."
