# Expected: Contested Narrative

Purpose: verify the group prompt detects conflicting accounts of the same core
event without treating the conflict as a reason to filter the documents.

Expected topic:

- North Reach Capital conference-room incident: `tip1.txt`, `tip2.txt`,
  `tip3.txt`

Expected group heuristics:

- `contested_narrative`: high
- `shared_references`: high or medium
- `corroboration`: medium or low, because the accounts overlap but conflict
- `coordinated_framing`: low
- `timeline_coherence`: medium or high

Checks:

- All documents should group under one incident/topic.
- Group explanation should identify that the same 14 April 2026 event is
  described from competing perspectives.
- The documents should generally pass filtering; emotional tone should not be
  high in both negative heuristics.
