# stayly

A standalone, self-hostable **WebMCP travel provider** (accommodation discovery & reservation).

stayly is an independent web application with two modes that share one state:

- **Human mode** — a plain HTMX UI (search and book).
- **Agent mode** — the same capabilities exposed to AI agents through the WebMCP `document.modelContext` API.

It is consumed by [Concierge](../concierge), the WebMCP host, which embeds stayly as a cross-origin iframe, discovers its tools with `getTools({ fromOrigins })`, and executes them with `executeTool()`.

## Run

```bash
go run .
# -> http://localhost:8082
```

## WebMCP tools

```text
search_stays
get_stay_details
check_room_availability
reserve_room
modify_reservation
cancel_reservation
```

## Data sources

- **Hotelbeds APItude** (live hotel offers & rates, test sandbox) — when `HOTELBEDS_API_KEY`/`HOTELBEDS_SECRET` are set.
- **Photon / OpenStreetMap** (real hotel names) — keyless.

## Configuration

| Var | Default | Purpose |
|---|---|---|
| `PORT` | `8082` | Listen port |
| `EXPOSED_ORIGINS` | `http://localhost:8080` | Origins allowed to see/execute the tools |
| `HOTELBEDS_API_KEY` | — | Optional live hotel offers |
| `HOTELBEDS_SECRET` | — | Optional live hotel offers |
| `ORIGIN_TRIAL_TOKEN` | — | Optional WebMCP origin-trial token |

## Golden demo data

Hotel Lumière, Le Marais, 5 nights, $1,180.

See the [Concierge README](../concierge/README.md) for the full architecture and the golden trip.
