# Erdős Problem Solver AI Agent

## Overview
A Go CLI agent that fetches open Erdős problems from the community database, sends them to an LLM for proof/disproof attempts, and saves solutions.

## Architecture

```
erdos-agent/
├── main.go                    # CLI entry point — orchestrates the full pipeline
├── CLAUDE.md                  # This file
├── go.mod / go.sum
├── internal/
│   ├── keystore/
│   │   └── keystore.go        # Secure API key storage (OS keychain via encrypted file)
│   ├── models/
│   │   └── problem.go         # Data types: Problem, Status, etc.
│   ├── scraper/
│   │   └── scraper.go         # Fetches problems.yaml + scrapes problem descriptions from erdosproblems.com
│   └── solver/
│       └── solver.go          # Constructs prompts, calls LLM via gollm, returns solutions
└── solns/                     # Output directory: {problem_id}-{attempt_id}.md
```

## Tech Decisions

| Concern           | Choice                              | Rationale                                                                 |
|--------------------|--------------------------------------|---------------------------------------------------------------------------|
| Language           | Go 1.21+                            | User requirement                                                          |
| LLM Library        | `github.com/teilomillet/gollm`       | Unified interface for OpenAI/Anthropic/Groq/Ollama; clean prompt API      |
| YAML Parsing       | `gopkg.in/yaml.v3`                   | Standard Go YAML library                                                  |
| HTML Scraping      | `golang.org/x/net/html`              | stdlib-adjacent HTML tokenizer, no heavy deps                             |
| Secure Input       | `golang.org/x/term`                  | For reading API key without echoing to terminal                           |
| Key Storage        | AES-256-GCM encrypted file           | Stored in `~/.erdos-agent/key.enc`, machine-derived encryption key        |

## Data Source
- **Problem metadata** (number, prize, status, tags): fetched from GitHub raw YAML
  `https://raw.githubusercontent.com/teorth/erdosproblems/main/data/problems.yaml`
- **Problem descriptions**: scraped from `https://www.erdosproblems.com/{number}`
  The problem text lives inside the page HTML and requires parsing the rendered content.

## Prompt Template
```
Don't search the internet. This is a test to see how well you can craft non-trivial, novel and creative proofs given a "number theory and primitive sets" math problem. Provide a full unconditional proof or disproof of the problem.

{{problem}}

REMEMBER - this unconditional argument may require non-trivial, creative and novel elements.
```

## CLI Flow
1. Agent starts → checks for stored API key
2. If no key: prompts user for provider + API key (masked input), stores encrypted
3. Fetches `problems.yaml` from GitHub
4. Filters problems: `status.state == "open"` (and optionally has prize)
5. Lists problems with prize/status info
6. User selects problem(s) to solve
7. Scrapes full problem description from erdosproblems.com
8. Sends prompt to LLM via gollm
9. Saves response to `solns/{problem_id}-{attempt_id}.md`

## Build & Run
```bash
go build -o erdos-agent .
./erdos-agent
```

## Key Conventions
- All error handling uses early returns with `fmt.Errorf` wrapping
- Context is threaded through for cancellation support
- No global state; dependencies are passed explicitly
