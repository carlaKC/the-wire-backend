# Palantir for the People - Backend

Whistleblowers protect our freedoms.
We protect their truth.

## About

This repository contains the backend implementation of a whistleblower
document classification and triage system. 

This tool is:
* A classification system that helps to prioritize sub-groups in large
  document dumps.
* A grading system that ranks documents on their ability to be
  externally verified.
* A filtering mechanism that removes documents that contradict
  themselves.
 
This tool is not:
* A secure submission platform: there are several well considered,
  open source projects that are dedicated to this problem.
* A "truth" oracle: LLMs are very weak at identifying facts that are
  not part of their training data. We largely expect the information
  in tips to be outside of the public domain, so they will be very
  difficult to verify even with retrieval augmented generation
  (if you could google this information, it wouldn't need to be whistle
  blown).
* A replacement for good journalism.

## Methodology

We run several rounds of LLM driven classification:
- Case analysis: processes the entire document dump to identify high
  level topics that the documents belong to.
- Topic analysis: processes all the documents that belong in a single
  topic to characterize the case being made.
- Per document analysis: individual grading of documents to identify
  any red flags and highlight information rich characteristics.

## Wishlist

This is a hackathon project! There are many features that make this
more real world robust:
1. Local LLM: the most private way to use this service would be to run
   with a local LLM so that data remains completely in your control.
2. Robust filtering: there is an obvious attack on this system
   where an adversary submits large volumes of documents to exhaust
   LLM tokens. Right now we process all documents - this can be
   made more robust with random sampling and pre-processing.
3. File type analysis: the MVP only supports text. There is a whole
   world of additional authenticity verification we can do with documents
   and photos that provide extra metadata.
4. Narrative detection: if a whistleblower's evidence is under attack,
   it's likely that a system will be flooded with contradictory
   information. The ability to identify documents associated with each
   narrative, and their respective strength, will be helpful to filter
   out this noise.

## Running this Code

Prerequisites:
* Golang installed

LLM integration:
* Maple API key and [proxy](https://github.com/opensecretcloud/maple-proxy)
* Claude API key

To install:
```
go build
```

### To Run with Maple

Set the following environment variables:
```
MAPLE_API_KEY={secret}
```

Run:
```
./backend --nomock
```

### To Run with Claude

Note: This mode will report your data to Anthropic! It is primarily
supported for testing and demonstration purposes.

Set the following environment variables:
```
ANTHROPIC_API_KEY={secret}
```

Run:
```
./backend --nomock --claude
```

### To run with Mocked Data

This mode returns hardcoded data to assist with testing and development
without burning tokens.

```
./backend
```
