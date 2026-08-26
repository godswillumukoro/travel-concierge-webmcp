# Concierge

> **Your trip. One conversation.**

Concierge is an **AI travel agent that plans and books an entire trip by directly using the WebMCP capabilities exposed by independent travel websites** — FlyNow (flights), Stayly (stays), Dine (restaurants), Explore (experiences) and Ride (transport).

The user gives Concierge a goal. Concierge discovers the providers' WebMCP tools, reasons about the goal, calls those tools, asks the human for approval when necessary, and maintains one coherent trip across otherwise independent websites.

---

## The core idea

WebMCP (Web Model Context Protocol) lets a web page expose *capabilities* — callable, schema-typed tools — to AI agents via `document.modelContext`. A top-level page can discover and execute tools from **cross-origin iframes** with `getTools({ fromOrigins })` and `executeTool()`, provided the provider explicitly exposes the tools and the iframe grants the `tools` permission.

Concierge is therefore **a WebMCP host**, not a backend MCP client:

```text
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

The providers stay independent. WebMCP is the interoperability layer.

---

## Architecture

### What runs in the browser
- Concierge UI (chat, approvals, itinerary, inspector)
- Provider iframes (each with `allow="tools"`)
- **WebMCP discovery** — `document.modelContext.getTools({ fromOrigins })`
- **WebMCP execution** — `document.modelContext.executeTool(tool, JSON.stringify(args))`
- Approval gating (price vs. threshold)
- Trip state / price indexing

### What runs in the Concierge backend (Go)
- The agent "next step" endpoint (`POST /api/agent`)
- Deterministic planner (default, offline) **or** an OpenAI-compatible model call
- Secrets / API key
- Activity log (rendered via HTMX)

### What runs in each provider backend (Go)
- Deterministic mock inventory
- The *real* booking/search state (the same state the human UI mutates)
- HTML + HTMX human UI, and JSON endpoints used by the WebMCP tool callbacks

The AI model never executes JavaScript and never fabricates results. It emits a tool call; the browser executes the matching WebMCP tool; the structured result flows back to the loop.

---

## Repository layout — *independent apps, not a monorepo*

Each application is a **separate, self-hostable Go service** with its own `go.mod` and no shared code. They live as sibling directories:

```text
code/
├── concierge/   ← this app (WebMCP host)         http://localhost:8080
├── flynow/      ← flights provider               http://localhost:8081
├── stayly/      ← stays provider                 http://localhost:8082
├── dine/        ← restaurants provider           http://localhost:8083
├── explore/     ← experiences provider           http://localhost:8084
└── ride/        ← transport provider             http://localhost:8085
```

Each provider runs in two modes — **human mode** (HTMX UI) and **agent mode** (WebMCP) — and both modes mutate the *same* application state.

> Using different ports on `localhost` makes each app a distinct origin, which is what exercises WebMCP's cross-origin story. `localhost` is a secure context, which WebMCP requires.

## Real data & free APIs

Pricing and inventory are mocked (there is no free, keyless flight/hotel booking API), but locations, places and routing are **real** where free keyless services exist:

| Service | Real data source |
|---|---|
| **Ride** | Photon place autocomplete/geocoding + OSRM road routing → real distance & duration, mock fare |
| **FlyNow** | real airport database (codes, cities, coordinates) + great-circle distance |
| **Stayly** | Photon `osm_tag=tourism:hotel` → real hotels near the destination |
| **Dine** | Photon `osm_tag=amenity:restaurant` → real restaurants |
| **Explore** | Photon `osm_tag=tourism:attraction/museum` → real attractions |

Every external call has a cache, a timeout, and a deterministic fallback, so the demo never breaks offline. (Google Maps Places can be swapped in for Photon if you have a billing-enabled API key — ask if you want that wired up.)

---

## Requirements

- **Go 1.22+** (developed with 1.26)
- **Chrome 149+** with WebMCP enabled. WebMCP is currently gated; enable it either way:
  - **Feature flag (simplest, local):** launch Chrome with `--enable-features=WebMCP`
  - **Origin trial:** provide a token to each app via `ORIGIN_TRIAL_TOKEN` (see https://developer.chrome.com/blog/ai-webmcp-origin-trial)

---

## Running locally

One command starts all six apps:

```bash
cd concierge
./scripts/dev.sh
```

Or run each independently (that's the point — each is self-hostable):

```bash
cd flynow   && go run .    # http://localhost:8081
cd stayly   && go run .    # http://localhost:8082
cd dine     && go run .    # http://localhost:8083
cd explore  && go run .    # http://localhost:8084
cd ride     && go run .    # http://localhost:8085
cd concierge && go run .   # http://localhost:8080
```

Open **http://localhost:8080** in Chrome (with WebMCP enabled). The "Connected services" panel should show all five providers with a green dot and their tool counts.

## Running with Docker

Each app ships a `Dockerfile`; the parent directory has a `docker-compose.yml` that runs all six as distinct origins:

```bash
cd /Users/gogo/code          # parent of the six app directories
cp .env.example .env         # optional: fill in API keys

docker compose up --build
# -> same URLs as above: http://localhost:8080 .. :8085
```

Notes:
- Each service maps to its usual `localhost` port, so WebMCP's cross-origin story works unchanged (different ports = different origins, and `localhost` is a secure context).
- `EXPOSED_ORIGINS` is set to `http://localhost:8080` for every provider, so they expose their tools to the Concierge origin.
- Optional keys (`OPENAI_API_KEY`, `DUFFEL_API_KEY`, `AMADEUS_CLIENT_ID/SECRET`, `ORIGIN_TRIAL_TOKEN`) are read from `.env`; without them everything still runs on deterministic mock + Photon/OSRM data.

---

## Environment variables

### All apps
| Var | Default | Purpose |
|---|---|---|
| `PORT` | per app | Listen port |
| `ORIGIN_TRIAL_TOKEN` | — | Optional WebMCP origin-trial token header |

### Providers (FlyNow, Stayly, Dine, Explore, Ride)
| Var | Default | Purpose |
|---|---|---|
| `EXPOSED_ORIGINS` | `http://localhost:8080` | Comma-separated origins allowed to see/execute this provider's WebMCP tools |

### Concierge
| Var | Default | Purpose |
|---|---|---|
| `PROVIDERS` | built-in 5 providers | JSON array `[{name,origin,category,icon}]` |
| `OPENAI_API_KEY` | — | If set, drive the agent with a model; if unset, the deterministic planner runs |
| `OPENAI_BASE_URL` | `https://api.openai.com/v1` | Any OpenAI-compatible endpoint (OpenAI, Gemini, Ollama, …) |
| `OPENAI_MODEL` | `gpt-4o-mini` | Model name |

---

## Testing

**Integration test (no browser needed)** — drives the planner against the real provider APIs and asserts the golden trip and the rejection fallback:

```bash
cd concierge
./scripts/e2e_test.py
```

**Full browser end-to-end (real WebMCP)** — runs the entire golden trip through actual cross-origin `executeTool()` calls, auto-approving for CI:

```bash
# with all apps running:
chrome --headless=new --enable-features=WebMCP --virtual-time-budget=25000 --dump-dom \
  "http://localhost:8080/?autopilot=1"
```

`?autopilot=1` is the deterministic smoke test: it fills the golden prompt, runs the loop, and auto-approves. Without it, the normal human-approval flow applies.

---

## The demo (acceptance test)

1. Open `http://localhost:8080` in Chrome with WebMCP enabled.
2. Open **⚙ Services** → see FlyNow / Stayly / Dine / Explore / Ride all `WebMCP ●` with tool counts.
3. Type (or click the quick prompt):

   > Plan a five-day trip to Paris for two people under $3,500. We want a boutique stay, great food, one memorable experience and airport transport. Automatically book anything under $100 and ask me before larger purchases.

4. Watch the activity log show the agent searching FlyNow → Stayly → Dine → Explore → Ride.
5. **Approve** the flight ($842) and hotel ($1,180); note the $96 experience and ride (live-computed from the real route) are auto-booked under the $100 threshold.
6. See the unified itinerary — the ride's distance, duration and fare come from real geocoding + routing (Photon + OSRM), so the exact total varies slightly with the route.

That last point is the real acceptance criterion: *within three minutes, it's clear why WebMCP matters.*

---

## WebMCP notes worth knowing

- The current API is `document.modelContext` (`navigator.modelContext` is deprecated).
- `executeTool(tool, input)` takes a **JSON string** and resolves with a **stringified result** — Concierge `JSON.parse`s it.
- All three methods (`registerTool`, `getTools`, `executeTool`) require an **origin-keyed agent cluster**, which is why every app sends `Origin-Agent-Cluster: ?1`. This matters on `localhost`, where same-site cross-origin frames would otherwise share a cluster.
- Tools are **origin-scoped**, not globally unique. Stayly and Dine both expose `cancel_reservation`; they are disambiguated by `origin`, exactly as the spec intends.
- `readOnlyHint` annotations mark search/read tools vs. write/booking tools; Concierge gates the write tools behind the human-approval threshold.

## Security / scope

This is a controlled demonstration environment. No real payments, APIs, or inventory. Prompt-injection and tool-annotation hardening are noted in the WebMCP spec and out of scope for the demo. Provider `execute` callbacks validate their inputs server-side.

## License

MIT — see [LICENSE](LICENSE).
