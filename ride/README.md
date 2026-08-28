# Ride

A standalone, self-hostable **WebMCP travel provider** (transportation).

Ride is the most "real" provider: pickup/dropoff **autocomplete**, **geocoding** and **road routing** come from free, keyless APIs (Photon + OSRM), so distance, duration and ETA are real. The fare is a deterministic function of the real route; booking is mocked (no payment).

- **Human mode** — HTMX UI with a type-ahead place dropdown (e.g. "Osapa, Lagos" → "Victoria Island, Lagos").
- **Agent mode** — the same capabilities exposed to AI agents through the WebMCP `document.modelContext` API.

It is consumed by [Concierge](../concierge), which embeds Ride as a cross-origin iframe, discovers its tools with `getTools({ fromOrigins })`, and executes them with `executeTool()`.

## Run

```bash
go run .
# -> http://localhost:8085
```

Try booking **Osapa → Victoria Island**: type in the pickup/dropoff boxes, pick a suggestion, search, then book (no real payment).

## WebMCP tools

```text
search_rides
get_ride_quote
book_ride
modify_ride
cancel_ride
```

## Configuration

| Var | Default | Purpose |
|---|---|---|
| `PORT` | `8085` | Listen port |
| `EXPOSED_ORIGINS` | `http://localhost:8080` | Origins allowed to see/execute the tools |
| `ORIGIN_TRIAL_TOKEN` | — | Optional WebMCP origin-trial token |

## Data sources

- **Photon** (free, keyless) — place autocomplete + geocoding.
- **OSRM** (free, keyless) — road distance & duration.
- Built-in gazetteer + great-circle fallback when offline.

See the [Concierge README](../concierge/README.md) for the full architecture.
