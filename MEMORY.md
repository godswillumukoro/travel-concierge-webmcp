# Concierge — Memory Bank

> Living project memory. **Update this file whenever I change architecture, add a
> feature, or learn something.** Last updated: 2026-08-26.

---

## 1. What this is

Concierge is an **AI travel agent that plans and books a whole trip by directly
using the WebMCP tools exposed by independent travel websites** — FlyNow
(flights), Stayly (stays), Dine (restaurants), Explore (experiences), Ride
(transport). The current PRD is **v2.1** (real-API data layer: Duffel, Hotelbeds,
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
- **Needs keys (obtained/verified):** Duffel (flights, test mode) — verified with
  a real key. OpenAI — verified. Hotelbeds APItude — verified (live hotel offers).
  Google Maps Places needs a billing key (Photon is the keyless default).

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
| flynow | search/book/cancel + airport autocomplete | 6 tools | ✅ real airports + live Duffel offers (v2, verified) | mock + live airports + Duffel |
| stayly | search/reserve/cancel | 6 tools | ✅ Photon hotels + live Hotelbeds offers (verified) | mock + live hotels |
| dine | search/book/cancel | 5 tools | ✅ Photon restaurants | mock + live restaurants |
| explore | search/book/cancel | 5 tools | ✅ Photon museums | mock + live attractions |
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
| Inspector (connected services + tool counts) | ✅ done — per-provider tool list (read/write badges) on click |
| Activity log | ✅ done (HTMX) |
| **Real API: Ride (Photon + OSRM)** | ✅ done |
| **Real API: Dine/Explore (Photon)** | ✅ done |
| **Real API: Stayly (Photon hotels + Hotelbeds)** | ✅ verified: live Hotelbeds offers (signature auth, geolocation search) |
| **Real API: FlyNow (real airports + Duffel)** | ✅ verified: live Duffel offers (v2, `data`-wrapped, `Duffel-Version: v2`) |
| API rate-limit/timeout/fallback handling | ✅ all live clients have timeout + mock fallback (caching in ride/live.go) |
| Provider `api_backend` metadata shown in UI (PRD §32/§33) | ✅ done ("Live data: …" badge per provider) |
| Disruption recovery (PRD §25/Phase 6) | ✅ done — rebooks flight + shifts ride + moves dinner via a "Simulate flight cancellation" button |
| Preference-aware selection (boutique/great food) | ✅ done — planner scores candidates by parsed preferences + rating + budget |
| LLM mode live-tested | ✅ verified: gpt-4o-mini drives tool calls end-to-end (date now injected into prompt) |
| Demo video / demo script | ✅ script done (DEMO_SCRIPT.md); video recording pending |

---

## 7. Task list (detailed, in priority order)

### In progress / next
1. ~~FlyNow real data~~ ✅ real airports (autocomplete) + great-circle distance/duration.
2. ~~Dine real data~~ ✅ Photon `amenity:restaurant` (real restaurants, bookable).
3. ~~Explore real data~~ ✅ Photon `tourism:museum` (real museums, bookable).
4. ~~Stayly real data~~ ✅ Photon `tourism:hotel` (real hotels, bookable).
5. ~~Duffel scaffold (FlyNow)~~ ✅ **verified with a real key.** Fixed API drift: `Duffel-Version: v2`,
   body nested under `data`, offers on the offer_requests response (`data.offers`),
   segment-level `departing_at`/`arriving_at`/`operating_carrier_flight_number`.
6. ~~Amadeus scaffold (Stayly)~~ → replaced with **Hotelbeds APItude** (Amadeus self-service
   decommissioned). ✅ verified with the user's test key: signature auth
   (`X-Signature` = SHA256(key+secret+ts)), `POST /hotel-api/1.0/hotels` geolocation
   search → real hotels + rates mapped to `Stay` (`source:"hotelbeds"`).

### Then
7. ~~`api_backend` metadata + "Live data: X" badge~~ ✅ done.
8. ~~Inspector: click provider → list tools (read/write badges)~~ ✅ done.
9. ~~Preference-aware planner~~ ✅ parses preferences (boutique/great food/memorable/
   luxury/cheapest/direct/museum…) and scores candidates by rating + keywords + budget.
   Golden picks still resolve for the golden prompt; variations verified (cheapest→Turkish,
   luxury→premium ride, museum→Louvre).
10. ~~LLM mode live test~~ ✅ verified with `OPENAI_API_KEY` (gpt-4o-mini): model emits
    valid tool calls, loop executes them. Fixed: inject today's date into the system
    prompt. (Minor: model can be repetitive; a stronger prompt/tools description helps.)
11. ~~Disruption recovery~~ ✅ "⚠ Simulate flight cancellation" button → cancels the flight,
    planner rebooks the next-best flight, shifts the ride to the new arrival+45m, moves
    dinner to 9:30 PM, then prints an updated itinerary. Verified: rebooks KLM KL-578.
12. Deployment: docker-compose / multi-origin HTTPS setup + origin-trial tokens.
    ✅ docker-compose + per-app Dockerfiles done (parent `docker-compose.yml`).
    Remaining: production multi-origin HTTPS (distinct domains + TLS + origin-trial tokens).
13. ~~Demo video + script~~ ✅ demo script written (concierge/DEMO_SCRIPT.md, ~3 min core +
    disruption + preferences); the video recording itself is still pending.

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
- flynow: `DUFFEL_API_KEY` (optional live offers).
- stayly: `HOTELBEDS_API_KEY` / `HOTELBEDS_SECRET` (optional live offers).

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
- **Human UI vs agent JSON parity** — the HTMX `/search` handlers and the JSON
  `/api/*` handlers must BOTH do live enrichment, or the human UI shows mock-only.
  Fixed: both now include Duffel/Hotelbeds/Photon with a "live · X" badge.
