# explore

A standalone, self-hostable **WebMCP travel provider** (activities & experiences).

explore is an independent web application with two modes that share one state:

- **Human mode** — a plain HTMX UI (search and book).
- **Agent mode** — the same capabilities exposed to AI agents through the WebMCP `document.modelContext` API.

It is consumed by [Concierge](../concierge), the WebMCP host, which embeds explore as a cross-origin iframe, discovers its tools with `getTools({ fromOrigins })`, and executes them with `executeTool()`.

## Run

```bash
go run .
# -> http://localhost:8084
```

## WebMCP tools

```text
search_experiences
get_experience_details
check_experience_availability
book_experience
cancel_booking
```

## Data sources

- **Photon / OpenStreetMap** (real attractions & museums near the destination) — keyless.

## Configuration

| Var | Default | Purpose |
|---|---|---|
| `PORT` | `8084` | Listen port |
| `EXPOSED_ORIGINS` | `http://localhost:8080` | Origins allowed to see/execute the tools |
| `ORIGIN_TRIAL_TOKEN` | — | Optional WebMCP origin-trial token |

## Golden demo data

Seine Evening Cruise, $48/person ($96 for two).

See the [Concierge README](../concierge/README.md) for the full architecture and the golden trip.
