# Travel Concierge (WebMCP)

> **Your trip. One conversation.**

**Concierge** is an AI travel agent that plans and books an entire trip by directly using the **WebMCP** capabilities exposed by independent travel websites — backed by live data from real APIs.

A top-level page embeds five providers as cross-origin iframes, discovers their tools with `document.modelContext.getTools({ fromOrigins })`, and executes them with `executeTool()`. The agent composes those capabilities into one coherent trip, keeping the human in control of important decisions.

```
                    HUMAN
                      │
                      ▼
               ┌────────────┐
               │ CONCIERGE  │  (WebMCP host + agent loop)
               └─────┬──────┘
                     │  document.modelContext
        ┌────────┬───┼───┬─────────┬────────┐
        ▼        ▼   ▼   ▼         ▼        ▼
     FlyNow   Stayly Dine Explore   Ride    (cross-origin iframes)
     WebMCP   WebMCP WebMCP WebMCP  WebMCP
```

## Six independent, self-hostable apps

Each app is a standalone Go + HTMX service (its own `go.mod`, no shared code) with **two modes** that share one state: a human HTMX UI and the same capabilities exposed as WebMCP tools.

| App | What it does | Live data |
|---|---|---|
| [`concierge`](concierge) | WebMCP host, agent loop, approvals, itinerary | OpenAI (or deterministic planner) |
| [`flynow`](flynow) | Flight search & booking | Duffel (test mode) |
| [`stayly`](stayly) | Accommodation | Hotelbeds + Photon (OSM) |
| [`dine`](dine) | Restaurants | Photon (OSM) |
| [`explore`](explore) | Experiences & attractions | Photon (OSM) |
| [`ride`](ride) | Transportation | Photon geocoding + OSRM routing |

## Quick start

```bash
# run everything (loads concierge/.env if present)
cd concierge && ./scripts/dev.sh

# or with Docker
docker compose up --build
```

Open **http://localhost:8080** in Chrome launched with WebMCP enabled:

```bash
chrome --enable-features=WebMCP
```

Keys are optional — without them the apps still run on deterministic mock data (the "golden trip" demo) plus keyless Photon/OSRM data. To enable live data, copy `.env.example` to `.env` and fill in `OPENAI_API_KEY`, `DUFFEL_API_KEY`, `HOTELBEDS_API_KEY`/`HOTELBEDS_SECRET`.

## Architecture & details

See [`concierge/README.md`](concierge/README.md) for the full architecture, the WebMCP implementation, environment variables, testing, and the demo script. `concierge/MEMORY.md` is the living project memory bank.

## License

MIT — see each app's `LICENSE`.
