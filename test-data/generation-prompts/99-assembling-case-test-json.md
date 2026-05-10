# Assembling a `case-test.json` from generated tips

The prompts in this directory produce raw text for individual tips. The API ingests a JSON shape — see `API.md` `POST /cases`. Once you have your tips as `tip1.txt`, `tip2.txt`, … in a directory, build the request body from that directory:

```sh
for f in tip*.txt; do
  jq -Rs --arg fn "$f" '{filename: $fn, content: .}' "$f"
done | jq -s '{documents: .}' > case-test.json
```

`-R` reads each file as raw text; `-s` slurps the per-file objects into the `documents` array.

Then submit:

```sh
curl -X POST http://localhost:8080/api/v1/cases \
  -H 'Content-Type: application/json' \
  -d @case-test.json
```

Drop the result into `test-data/<batch-name>/` alongside the existing `batch1` and `batch2` for repeatability.
