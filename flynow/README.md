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

- Deterministic mock inventory, available without API keys (also shown in the human UI).
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

## Search options

The human search form and WebMCP `search_flights` use the same search logic.
JSON callers POST to `/api/search_flights`:

```json
{
  "origin": "LOS",
  "destination": "CDG",
  "departure_date": "2026-09-15",
  "return_date": "2026-09-20",
  "travelers": 2,
  "nonstop": true,
  "sort_by_price": "asc"
}
```

- `nonstop` is optional; omitted or `false` includes connecting flights. `true`
  includes only zero-stop flights. For Duffel round trips, every leg must be
  nonstop. The stock mock inventory has no nonstop options, so this filter
  returns an empty list in mock mode.
- `sort_by_price` is optional: `asc` for lowest first, `desc` for highest first.
  Omitted (or empty in the HTTP API/form) keeps the original ordering: mock
  inventory order followed by Duffel's cheapest-first offers (capped at 12).
  Sorting applies to the combined results and preserves equal-price order.
  Other values return HTTP 400. Filtering and optional sorting apply after
  Duffel's existing cap; they do not request additional offers.
- Prices remain numeric whole currency units, including the existing Duffel
  rounding, and are sorted numerically without currency conversion. This
  assumes comparable currencies; mixed currencies are not exchange-rate ranked.
  `price_display` adds a currency code and two decimal places, e.g. `USD 842.00`,
  matching the search cards. This does not add fractional price precision.
- Search results expose `flight_id` instead of `id`; pass it as `flight_id` to
  details, availability, hold, and booking tools. This is a breaking change for
  callers reading search-result `id`. Other endpoints retain their existing
  serialized flight shape.
- Mock routes, dates, and fares remain fixed demo data; dates and traveler counts
  do not alter mock inventory prices. Nonpositive traveler counts default to 2.
  Duffel errors still fall back to mock results.

Run only the focused search tests (the existing integration test also has
`Search` in its name, so explicit files exclude it):

```bash
go test -run Search -v search_test.go main.go search.go duffel.go airports.go
```

The existing `TestFlightSearchToBooking` integration test pins the old search
`id` contract. It is left unchanged and requires a caller-contract migration
before it can pass with `flight_id` results.
