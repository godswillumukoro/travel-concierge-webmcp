# dine

A standalone, self-hostable **WebMCP travel provider** (restaurant discovery & reservation).

dine is an independent web application with two modes that share one state:

- **Human mode** — a plain HTMX UI (search and book).
- **Agent mode** — the same capabilities exposed to AI agents through the WebMCP `document.modelContext` API.

It is consumed by [Concierge](../concierge), the WebMCP host, which embeds dine as a cross-origin iframe, discovers its tools with `getTools({ fromOrigins })`, and executes them with `executeTool()`.

## Run

```bash
go run .
# -> http://localhost:8083
```

## WebMCP tools

```text
search_restaurants
get_restaurant_details
check_table_availability
book_table
cancel_reservation
```

## Data sources

- **Photon / OpenStreetMap** (real restaurants near the destination) — keyless.

## Configuration

| Var | Default | Purpose |
|---|---|---|
| `PORT` | `8083` | Listen port |
| `EXPOSED_ORIGINS` | `http://localhost:8080` | Origins allowed to see/execute the tools |
| `ORIGIN_TRIAL_TOKEN` | — | Optional WebMCP origin-trial token |

## Golden demo data

Le Jardin, 2 people, 7:30 PM, $120.

See the [Concierge README](../concierge/README.md) for the full architecture and the golden trip.
