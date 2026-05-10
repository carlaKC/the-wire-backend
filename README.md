# Palantir for the People - Backend

Whistleblowers protect our freedoms.
We protect their truth.

## About

This repository contains the backend implementation of a whistleblower
document classification and triage system.

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
