# Concierge — Memory Bank

> Living project memory. **Update this file whenever I change architecture, add a
> feature, or learn something.** Last updated: 2026-08-26.

---

## 1. What this is

Concierge is an **AI travel agent that plans and books a whole trip by directly
using the WebMCP tools exposed by independent travel websites** — FlyNow
(flights), Stayly (stays), Dine (restaurants), Explore (experiences), Ride
(transport). The current PRD is **v2.1** (real-API data layer: Duffel, Amadeus,
Photon, OSRM).

Thesis: *Websites expose capabilities via WebMCP (interoperability layer) and are
backed by real APIs (data layer); Concierge composes them.*

---

## 2. Locked architecture decisions (do not silently change)

| # | Decision | Rationale / status |
|---|---|---|
| D1 | **Concierge is a WebMCP *host*, not a backend MCP client.** The browser discovers (`getTools`) and executes (`executeTool`) provider tools; the Go backend only does model calls/secrets/planner. | Core to the judging story. DONE. |
| D2 | **Six independent, self-hostable Go apps** (not a monorepo). | User explicitly overrode PRD §41 ("monorepo preferred"). Each app has its own `go.mod`, no shared code. DONE. |
| D3 | **Simple stack: Go + HTMX.** Vanilla JS only for the WebMCP orchestration loop (inherently browser-side). | User requirement. DONE. |
| D4 | **Same-origin state, two modes.** Each provider's human HTMX UI and WebMCP tools mutate the *same* in-memory state. | PRD §14/§47. DONE. |
| D5 | **Origin-keyed agent cluster required.** Every app sends `Origin-Agent-Cluster: ?1`. | WebMCP spec requires it; on localhost, same-site cross-origin iframes (`:8080` vs `:8081`) would otherwise share a cluster and `registerTool`/`getTools`/`executeTool` would reject. VERIFIED. |
| D6 | **Approvals enforced client-side** (browser), gated by price vs. threshold (default $100). Backend just emits tool calls. | Keeps approval auditable from real tool results. DONE. |
| D7 | **Deterministic planner is the default** when `OPENAI_API_KEY` is unset; LLM mode is the same loop but model-driven. | Makes the repo runnable offline. DONE. |
| D8 | **Real data where free/keyless; mock pricing/inventory always** (no free flight/hotel booking API). | PRD v2.1 + user's "make it feel real". Ride DONE; FlyNow/Stayly/Dine/Explore in progress. |
| D9 | **Golden trip = Lagos→Paris, 2 pax, Sep 15–20, $3,500, threshold $100.** Prices are totals; ride fare is now live-computed. | PRD §15/§21. DONE. |
| D10 | `document.modelContext` (not `navigator.modelContext`). `executeTool(tool, JSON.stringify(args))` and its result is a JSON string to `JSON.parse`. | Matches Chrome implementation + PRD §50. VERIFIED. |

---

## 3. Verified technical facts (Chrome 151 + this network)

- **WebMCP** works in Chrome 151 behind the `WebMCP` feature flag: launch with
  `--enable-features=WebMCP`. (Also available via origin trial; `ORIGIN_TRIAL_TOKEN`
  env on each app adds the header.)
- Cross-origin discovery works: `document.modelContext.getTools({fromOrigins})`
  returned **27 tools across 5 iframe origins**. Execution via `executeTool`
  placed real bookings. `iframe.allow = 'tools <origin>'` + provider
  `registerTool(tool, {exposedTo:[conciergeOrigin]})`.
- **Free keyless APIs — working here:** Photon (autocomplete/geocoding, incl.
  `osm_tag=tourism:hotel`, `osm_tag=amenity:restaurant`, `osm_tag=tourism:museum`),
  OSRM (`router.project-osrm.org` routing).
- **Free keyless APIs — BLOCKED here:** Nominatim ("Access denied" policy),
  Overpass (HTTP 406). Do not rely on these.
- **Needs keys (not yet obtained):** Duffel (flights, test mode), Amadeus
  (hotels, test mode), Google Maps Places (billing key). These must be
  env-key-gated with graceful mock fallback.

---

## 4. Repo layout & ports

```
/Users/gogo/code/
├── concierge/   ← WebMCP host + agent loop        :8080   (has scripts/, MEMORY.md, ecosystem README)
├── flynow/      ← flights                         :8081
├── stayly/      ← stays                           :8082
├── dine/        ← restaurants                     :8083
├── explore/     ← experiences                     :8084
└── ride/        ← transport (real Photon+OSRM)    :8085
```

Each is its own git repo. Sibling dirs like `broadcast-box/`, `lnaudit/`,
`paxform-web/`, etc. are unrelated pre-existing projects — do not touch.

---

## 5. Status per app (what's done)

| App | Human UI | WebMCP tools | Real-data layer | Backend |
|---|---|---|---|---|
| concierge | chat + approvals + itinerary + inspector + activity log (HTMX) | host: discovery + execution | n/a | agent endpoint: deterministic planner + optional OpenAI-compat LLM |
| flynow | search/book/cancel (HTMX) | 6 tools | ❌ **TODO** real airports + Duffel | deterministic mock inventory |
| stayly | search/reserve/cancel | 6 tools | ❌ **TODO** Photon hotels + Amadeus | deterministic mock |
| dine | search/book/cancel | 5 tools | ❌ **TODO** Photon restaurants | deterministic mock |
| explore | search/book/cancel | 5 tools | ❌ **TODO** Photon attractions | deterministic mock |
| ride | **autocomplete dropdown** + search/book/cancel | 5 tools | ✅ **DONE** Photon autocomplete + OSRM routing + mock fare | real geocode/route + gazetteer fallback |

**Already verified end-to-end:** golden trip (5 searches → 5 bookings → itinerary,
approvals at $100 threshold), rejection fallback (flight #1 rejected → KLM #2),
autopilot browser smoke test (`?autopilot=1`), `scripts/e2e_test.py`.

---

## 6. Audit vs PRD v2.1 (gap analysis)

| PRD item | Status |
|---|---|
| WebMCP cross-origin host (getTools/executeTool) | ✅ done |
| Agent loop (planner + LLM path) | ✅ planner done; LLM path written but **untested (no key)** |
| Human approval system + UI | ✅ done |
| Cross-provider trip id (`TRIP-001`) | ✅ done |
| Budget + threshold + itinerary card | ✅ done |
| Inspector (connected services + tool counts) | ✅ done (tool *lists/schemas* per provider not yet shown — PRD §10) |
| Activity log | ✅ done (HTMX) |
| **Real API: Ride (Photon + OSRM)** | ✅ done |
| **Real API: Dine/Explore (Photon)** | ❌ todo |
| **Real API: Stayly (Photon hotels + Amadeus)** | ❌ todo |
| **Real API: FlyNow (real airports + Duffel)** | ❌ todo |
| API rate-limit/timeout/fallback handling | ⚠️ Ride has cache+timeout+fallback; others pending |
| Provider `api_backend` metadata shown in UI (PRD §32/§33) | ❌ todo |
| Disruption recovery (PRD §25/Phase 6) | ❌ todo (stretch) |
| Preference-aware selection (boutique/great food) | ❌ todo (planner is golden-hardcoded) |
| LLM mode live-tested | ❌ todo (needs key) |
| Demo video / demo script | ❌ todo |

---

## 7. Task list (detailed, in priority order)

### In progress / next
1. **FlyNow real data** — embed real airport DB (code/city/name/lat/lon), add
   great-circle distance + est. duration to flight results, airport autocomplete
   in UI. *(self-contained, no network)*
2. **Dine real data** — Photon `osm_tag=amenity:restaurant` (biased to geocoded
   destination) → real restaurant names appended to search results; mark `source`.
3. **Explore real data** — Photon `osm_tag=tourism:museum/attraction` → real attractions.
4. **Stayly real data** — Photon `osm_tag=tourism:hotel` → real hotels.
5. **Duffel scaffold (FlyNow)** — env-key-gated live client (`DUFFEL_API_KEY`) with
   mock fallback; **needs a real test key to verify endpoint shape**.
6. **Amadeus scaffold (Stayly)** — OAuth token + hotel-search client
   (`AMADEUS_CLIENT_ID/SECRET`) with mock fallback; **needs key to verify**.

### Then
7. `api_backend` metadata per provider (PRD §32) + show "Live data: X" in the
   Concierge inspector (§33).
8. Inspector: click provider → list its tools + schemas (§10).
9. Preference-aware planner (honor "boutique", "great food", "memorable") instead
   of golden-hardcoded picks.
10. LLM mode live test + hardening (retries, malformed tool-call handling,
    `cancel_reservation` origin-collision note).
11. Disruption recovery (Phase 6): simulate flight cancellation → rebook + shift
    ride/dinner, ask approval.
12. Deployment: docker-compose / multi-origin HTTPS setup + origin-trial tokens.
13. Demo video + script (§39).

### Housekeeping (ongoing)
- `git init` + commit each feat per app (DONE for baseline; continue).
- Keep this MEMORY.md updated.
- READMEs per app + ecosystem README.

---

## 8. Commands

```bash
cd /Users/gogo/code/concierge
./scripts/dev.sh        # start all (Ctrl+C stops)
./scripts/stop.sh       # stop all (kills orphaned go-run children too)
python3 scripts/e2e_test.py   # integration test (planner + providers)

# browser smoke test (full WebMCP, auto-approves):
chrome --headless=new --enable-features=WebMCP --virtual-time-budget=25000 \
  --dump-dom "http://localhost:8080/?autopilot=1"
```

### Env vars
- All apps: `PORT`, `ORIGIN_TRIAL_TOKEN`.
- Providers: `EXPOSED_ORIGINS` (default `http://localhost:8080`).
- concierge: `PROVIDERS`, `OPENAI_API_KEY`, `OPENAI_BASE_URL`, `OPENAI_MODEL`.
- ride: uses Photon/OSRM keylessly.
- (planned) flynow: `DUFFEL_API_KEY`; stayly: `AMADEUS_CLIENT_ID`/`AMADEUS_CLIENT_SECRET`.

---

## 9. Gotchas / lessons

- **`go run` orphans compiled children** — killing the parent leaves the port bound.
  Always use `scripts/stop.sh` (kills by port) or `lsof -tiTCP:PORT`.
- **Gazetteer map iteration is random in Go** — never substring-match by ranging a
  map. Use exact → token → Photon → sorted-longest-substring (see ride `resolvePlace`).
- **Tool names are origin-scoped** (Stayly & Dine both expose `cancel_reservation`) —
  fine per spec, disambiguated by `origin`; the planner never calls them.
- **Photon "hotel Paris"** hits French "Hôtel de Ville" — must use
  `osm_tag=tourism:hotel` filter.
- Chrome needs `--enable-features=WebMCP` (or origin trial) — `document.modelContext`
  is otherwise `undefined`; the concierge UI shows a banner when missing.
