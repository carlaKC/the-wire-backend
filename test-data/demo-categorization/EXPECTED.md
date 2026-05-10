# Expected: Demo 1 — Categorization at Scale

Purpose: pitch demo. Demonstrates that the system can ingest ten
whistleblower documents from different reporters in different roles,
covering different programs and mechanisms, and group them into the two
underlying subject areas without any human guidance.

Expected topics and document counts:

- Defense contractor financial / quality misconduct: `tip1.txt`, `tip2.txt`,
  `tip3.txt`, `tip4.txt`, `tip5.txt` (5 docs)
- Pharmaceutical and life-sciences data integrity: `tip6.txt`, `tip7.txt`,
  `tip8.txt`, `tip9.txt`, `tip10.txt` (5 docs)

Checks:

- `GET /cases/{id}` should return exactly 2 topics with document counts of 5
  and 5.
- The defense topic should pull together five Vermilion Defense Systems
  reports of cost-reimbursable government cost-mischarging: cross-program
  labor mischarging on Hawkeye-X CPFF (tip1), unallowable costs in the
  indirect pool on the Aegis maintenance contract (tip2), subcontractor
  markup pass-through fraud on the FA8730 CPIF program (tip3), labor-
  category mischarging on an Army cost-plus task order (tip4), and travel /
  per-diem fraud flowing through G&A on cost-reimbursable work (tip5).
  Same company across all five tips, all on cost-reimbursable contracts,
  different reporters / programs / mechanisms, with a unified through-line:
  Vermilion billed the federal government for costs it should not have.
- The pharma topic should pull together five Caldera Pharmaceuticals
  reports of sponsor data integrity failures vs. FDA across the drug
  lifecycle: clinical trial AE recoding before NDA (tip6), preclinical tox
  data exclusion before IND (tip7), stability test re-analysis before ANDA
  approval (tip8), bioequivalence study site data manipulation in a
  Caldera-sponsored ANDA package (tip9, written from the CRO side), and
  post-market AE under-reporting (tip10). Same sponsor across all five tips
  (tip9 is a CRO monitor reporting on a Caldera-sponsored study), different
  stages of the drug lifecycle (preclinical, IND, NDA, ANDA, post-market)
  and different mechanisms, with a unified through-line: regulatory
  submission data does not reflect the underlying experimental record.
- Topics should not split by program / stage / mechanism / reporter role.
  They should consolidate at the level of subject area.
- Topic titles may differ from the labels above; what matters for the demo
  is that the document membership is 5/5 along these lines.
- Topic importance for both topics should land on `high`: both involve
  systemic fraud against the federal government and / or systemic patient
  safety risk.

Pitch beats during the live demo:

- Open the case detail. Show the two topics rendered next to ten distinct
  source documents.
- Click into each topic. Read the topic title and description, then click
  into one of the docs and show the underlying tip — make the point that
  the reporters are in different roles seeing different schemes, even
  though all the defense tips are at one company and all the pharma tips
  at one sponsor.
- Note that the system is not pattern-matching the company name alone —
  each tip uses different jargon, different programs, different
  contracting officers, different drug products / lifecycle stages.
- Compare against a hypothetical untouched corpus: ten files in a folder
  that an investigator would otherwise have to read sequentially.
