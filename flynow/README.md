# flynow

A standalone, self-hostable **WebMCP travel provider** (flight search & booking).

flynow is an independent web application with two modes that share one state:

- **Human mode** — a plain HTMX UI (search and book).
- **Agent mode** — the same capabilities exposed to AI agents through the WebMCP `document.modelContext` API.

It is consumed by [Concierge](../concierge), the WebMCP host, which embeds flynow as a cross-origin iframe, discovers its tools with `getTools({ fromOrigins })`, and executes them with `executeTool()`.

## Run

```bash
go run .
# -> http://localhost:8081
```

## WebMCP tools

```text
search_flights
get_flight_details
check_flight_availability
hold_flight
book_flight
cancel_flight
```

## Data sources

- **Duffel** (live flight offers, test mode) — when `DUFFEL_API_KEY` is set.
- Real airport database + great-circle distance/duration.

## Configuration

| Var | Default | Purpose |
|---|---|---|
| `PORT` | `8081` | Listen port |
| `EXPOSED_ORIGINS` | `http://localhost:8080` | Origins allowed to see/execute the tools |
| `DUFFEL_API_KEY` | — | Optional live flight offers |
| `ORIGIN_TRIAL_TOKEN` | — | Optional WebMCP origin-trial token |

## Golden demo data

Air France AF-104, Lagos → Paris, $842.

See the [Concierge README](../concierge/README.md) for the full architecture and the golden trip.
