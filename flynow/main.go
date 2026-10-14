// FlyNow — a standalone WebMCP travel provider (flight search & booking).
//
// FlyNow is an independent web application. It serves a human-facing UI
// (HTMX) and exposes the exact same capabilities to AI agents through the
// WebMCP `document.modelContext` API. Both modes mutate the same in-memory
// application state.
package main

import (
	"embed"
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed static
var staticFS embed.FS

// ---------------------------------------------------------------------------
// Domain model
// ---------------------------------------------------------------------------

type Flight struct {
	ID              string `json:"id"`
	Airline         string `json:"airline"`
	FlightNumber    string `json:"flight_number"`
	Origin          string `json:"origin"`
	Destination     string `json:"destination"`
	Departure       string `json:"departure"`
	Arrival         string `json:"arrival"`
	ReturnDeparture string `json:"return_departure"`
	ReturnArrival   string `json:"return_arrival"`
	Stops           int    `json:"stops"`
	Price           int    `json:"price"`
	Currency        string `json:"currency"`
	Refundable      bool   `json:"refundable"`
	Cabin           string `json:"cabin"`
	DistanceKm      float64 `json:"distance_km"`
	Duration        string  `json:"duration"`
	Source          string  `json:"source,omitempty"`
	LogoURL         string  `json:"logo_url,omitempty"`
}

type Booking struct {
	BookingID string `json:"booking_id"`
	TripID    string `json:"trip_id"`
	Status    string `json:"status"`
	Flight    Flight `json:"flight"`
	Travelers int    `json:"travelers"`
	Price     int    `json:"price"`
	Currency  string `json:"currency"`
	CreatedAt string `json:"created_at"`
}

// Deterministic mock inventory for the "golden trip" (Lagos -> Paris).
var flights = []Flight{
	{ID: "FL-001", Airline: "Air France", FlightNumber: "AF-104", Origin: "LOS", Destination: "CDG",
		Departure: "2026-09-15T09:20", Arrival: "2026-09-15T17:30", ReturnDeparture: "2026-09-20T10:00", ReturnArrival: "2026-09-20T18:10",
		Stops: 1, Price: 842, Currency: "USD", Refundable: true, Cabin: "Economy"},
	{ID: "FL-002", Airline: "KLM", FlightNumber: "KL-578", Origin: "LOS", Destination: "CDG",
		Departure: "2026-09-15T22:40", Arrival: "2026-09-16T06:10", ReturnDeparture: "2026-09-20T07:20", ReturnArrival: "2026-09-20T15:05",
		Stops: 1, Price: 865, Currency: "USD", Refundable: true, Cabin: "Economy"},
	{ID: "FL-003", Airline: "Lufthansa", FlightNumber: "LH-562", Origin: "LOS", Destination: "CDG",
		Departure: "2026-09-15T14:10", Arrival: "2026-09-15T21:45", ReturnDeparture: "2026-09-20T13:30", ReturnArrival: "2026-09-20T21:00",
		Stops: 1, Price: 890, Currency: "USD", Refundable: false, Cabin: "Economy"},
	{ID: "FL-004", Airline: "British Airways", FlightNumber: "BA-2082", Origin: "LOS", Destination: "CDG",
		Departure: "2026-09-15T08:00", Arrival: "2026-09-15T19:05", ReturnDeparture: "2026-09-20T09:40", ReturnArrival: "2026-09-20T20:55",
		Stops: 2, Price: 910, Currency: "USD", Refundable: true, Cabin: "Economy"},
	{ID: "FL-005", Airline: "Turkish Airlines", FlightNumber: "TK-624", Origin: "LOS", Destination: "CDG",
		Departure: "2026-09-15T01:35", Arrival: "2026-09-15T15:50", ReturnDeparture: "2026-09-20T04:10", ReturnArrival: "2026-09-20T18:30",
		Stops: 2, Price: 798, Currency: "USD", Refundable: false, Cabin: "Economy"},
}

// ---------------------------------------------------------------------------
// State
// ---------------------------------------------------------------------------

type Store struct {
	mu       sync.Mutex
	bookings map[string]Booking
	holds    map[string]string // holdID -> flightID
	live     map[string]Flight
	next     int
}

func newStore() *Store {
	return &Store{bookings: map[string]Booking{}, holds: map[string]string{}, live: map[string]Flight{}, next: 1}
}

func (s *Store) newID(prefix string) string {
	s.next++
	return prefix + "-" + strconv.Itoa(s.next)
}

// ---------------------------------------------------------------------------
// Server
// ---------------------------------------------------------------------------

type pageData struct {
	ExposedOriginsJS template.JS
	Airports         []string
}

func main() {
	port := getenv("PORT", "8081")
	exposed := splitOrigins(getenv("EXPOSED_ORIGINS", "http://localhost:8080"))
	store := newStore()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		servePage(w, exposed)
	})
	mux.HandleFunc("GET /static/", serveStatic)
	mux.HandleFunc("GET /api/airports", withJSON(store.handleAirports))

	// JSON API (used by both the human UI's HTMX and WebMCP tool execution).
	mux.HandleFunc("POST /api/search_flights", withJSON(store.searchFlights))
	mux.HandleFunc("POST /api/get_flight_details", withJSON(store.flightDetails))
	mux.HandleFunc("POST /api/check_flight_availability", withJSON(store.checkAvailability))
	mux.HandleFunc("POST /api/hold_flight", withJSON(store.holdFlight))
	mux.HandleFunc("POST /api/book_flight", withJSON(store.bookFlight))
	mux.HandleFunc("POST /api/cancel_flight", withJSON(store.cancelFlight))
	mux.HandleFunc("GET /api/state", withJSON(store.state))

	// HTMX endpoints (human UI fragments).
	mux.HandleFunc("POST /search", store.htmxSearch)
	mux.HandleFunc("POST /book", store.htmxBook)
	mux.HandleFunc("POST /cancel", store.htmxCancel)
	mux.HandleFunc("GET /bookings", store.htmxBookings)

	log.Printf("FlyNow listening on http://localhost:%s (exposing tools to %v)", port, exposed)
	log.Fatal(http.ListenAndServe(":"+port, withHeaders(mux)))
}

func withHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// WebMCP requires documents to live in an origin-keyed agent cluster.
		w.Header().Set("Origin-Agent-Cluster", "?1")
		w.Header().Set("Cache-Control", "no-store")
		if tok := os.Getenv("ORIGIN_TRIAL_TOKEN"); tok != "" {
			w.Header().Set("Origin-Trial", tok)
		}
		next.ServeHTTP(w, r)
	})
}

func servePage(w http.ResponseWriter, exposed []string) {
	tpl, err := template.ParseFS(staticFS, "static/index.html")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	js, _ := json.Marshal(exposed)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = tpl.Execute(w, pageData{ExposedOriginsJS: template.JS(js), Airports: allAirports()})
}

func serveStatic(w http.ResponseWriter, r *http.Request) {
	http.FileServer(http.FS(staticFS)).ServeHTTP(w, r)
}

func (s *Store) handleAirports(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	writeJSON(w, map[string]any{"airports": matchAirports(q)})
}

// ---------------------------------------------------------------------------
// Core logic (shared by JSON API and HTMX handlers)
// ---------------------------------------------------------------------------

func (s *Store) findFlight(id string) *Flight {
	for i := range flights {
		if flights[i].ID == id {
			return &flights[i]
		}
	}
	if f, ok := s.live[id]; ok {
		return &f
	}
	return nil
}

func (s *Store) searchFlights(w http.ResponseWriter, r *http.Request) {
	var in searchInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, 400, "invalid request: "+err.Error())
		return
	}
	if !in.validSort() {
		writeErr(w, 400, "sort_by_price must be asc or desc")
		return
	}
	results := s.search(in)
	out := make([]searchResult, 0, len(results))
	for _, f := range results {
		out = append(out, mapSearchResult(f))
	}
	writeJSON(w, map[string]any{"flights": out})
}

func (s *Store) flightDetails(w http.ResponseWriter, r *http.Request) {
	var in struct {
		FlightID string `json:"flight_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	f := s.findFlight(in.FlightID)
	if f == nil {
		writeErr(w, 404, "flight not found")
		return
	}
	if d, dur := flightDistance(f.Origin, f.Destination); d > 0 {
		f.DistanceKm = d
		f.Duration = dur
	}
	writeJSON(w, map[string]any{"flight": *f})
}

func (s *Store) checkAvailability(w http.ResponseWriter, r *http.Request) {
	var in struct {
		FlightID  string `json:"flight_id"`
		Travelers int    `json:"travelers"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	f := s.findFlight(in.FlightID)
	if f == nil {
		writeErr(w, 404, "flight not found")
		return
	}
	writeJSON(w, map[string]any{
		"flight_id": f.ID, "available": true, "seats_left": 7, "price": f.Price, "currency": f.Currency,
	})
}

func (s *Store) holdFlight(w http.ResponseWriter, r *http.Request) {
	var in struct {
		FlightID string `json:"flight_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	f := s.findFlight(in.FlightID)
	if f == nil {
		writeErr(w, 404, "flight not found")
		return
	}
	s.mu.Lock()
	holdID := s.newID("HLD")
	s.holds[holdID] = f.ID
	s.mu.Unlock()
	writeJSON(w, map[string]any{
		"hold_id": holdID, "flight_id": f.ID, "status": "HELD",
		"expires_at": time.Now().Add(15 * time.Minute).UTC().Format(time.RFC3339),
		"price": f.Price, "currency": f.Currency,
	})
}

func (s *Store) bookFlight(w http.ResponseWriter, r *http.Request) {
	var in struct {
		FlightID string `json:"flight_id"`
		HoldID   string `json:"hold_id"`
		TripID   string `json:"trip_id"`
		Travelers int   `json:"travelers"`
		Passenger string `json:"passenger_name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	if in.HoldID != "" {
		s.mu.Lock()
		if fid, ok := s.holds[in.HoldID]; ok {
			in.FlightID = fid
			delete(s.holds, in.HoldID)
		}
		s.mu.Unlock()
	}
	f := s.findFlight(in.FlightID)
	if f == nil {
		writeErr(w, 404, "flight not found")
		return
	}
	if in.Travelers <= 0 {
		in.Travelers = 2
	}
	if in.TripID == "" {
		in.TripID = "TRIP-001"
	}
	s.mu.Lock()
	id := s.newID("FL")
	b := Booking{BookingID: id, TripID: in.TripID, Status: "CONFIRMED", Flight: *f,
		Travelers: in.Travelers, Price: f.Price, Currency: f.Currency,
		CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	s.bookings[id] = b
	s.mu.Unlock()
	writeJSON(w, map[string]any{"booking": b})
}

func (s *Store) cancelFlight(w http.ResponseWriter, r *http.Request) {
	var in struct {
		BookingID string `json:"booking_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	s.mu.Lock()
	b, ok := s.bookings[in.BookingID]
	if ok {
		b.Status = "CANCELLED"
		s.bookings[in.BookingID] = b
	}
	s.mu.Unlock()
	if !ok {
		writeErr(w, 404, "booking not found")
		return
	}
	writeJSON(w, map[string]any{"booking": b})
}

func (s *Store) state(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	bs := make([]Booking, 0, len(s.bookings))
	for _, b := range s.bookings {
		bs = append(bs, b)
	}
	s.mu.Unlock()
	writeJSON(w, map[string]any{"bookings": bs, "flights": flights})
}

// ---------------------------------------------------------------------------
// HTMX fragments (human mode)
// ---------------------------------------------------------------------------

func (s *Store) htmxSearch(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	origin := resolveAirportCode(r.FormValue("origin"))
	dest := resolveAirportCode(r.FormValue("destination"))
	travelers, _ := strconv.Atoi(r.FormValue("travelers"))
	in := searchInput{
		Origin: origin, Destination: dest,
		DepartureDate: r.FormValue("departure_date"), ReturnDate: r.FormValue("return_date"),
		Travelers: travelers, Nonstop: r.FormValue("nonstop") == "true",
		SortByPrice: r.FormValue("sort_by_price"),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if !in.validSort() {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`<p class="muted">Price sort must be asc or desc.</p>`))
		return
	}
	if origin == "" || dest == "" {
		_, _ = w.Write([]byte(`<p class="muted">Enter an origin and destination to search flights.</p>`))
		return
	}
	results := s.search(in)
	if len(results) == 0 {
		_, _ = w.Write([]byte(`<p class="muted">No flights found for these search options.</p>`))
		return
	}
	var sb strings.Builder
	for _, f := range results {
		s.renderFlightCard(&sb, f)
	}
	_, _ = w.Write([]byte(sb.String()))
}

func (s *Store) renderFlightCard(sb *strings.Builder, f Flight) {
	logo := `<div class="card-img">✈</div>`
	if f.LogoURL != "" {
		logo = `<div class="card-img" style="background-image:url('` + f.LogoURL + `');background-size:contain;background-repeat:no-repeat;background-position:center;"></div>`
	}
	dist := ""
	if d, dur := flightDistance(f.Origin, f.Destination); d > 0 {
		dist = " · " + strconv.FormatFloat(d, 'f', 0, 64) + " km · " + dur
	}
	sb.WriteString(`<div class="card">`)
	sb.WriteString(logo)
	sb.WriteString(`<div class="card-body">`)
	badge := "mock"
	if f.Source == "duffel" {
		badge = "live · Duffel"
	}
	sb.WriteString(`<div class="card-top"><strong>` + f.Airline + `</strong> <span class="muted">` + f.FlightNumber + `</span><span class="badge">` + badge + `</span><span class="price">` + formatPrice(f.Currency, f.Price) + `</span></div>`)
	sb.WriteString(`<div class="row">` + f.Origin + ` → ` + f.Destination + `</div>`)
	sb.WriteString(`<div class="row muted">` + f.Departure + ` · ` + strconv.Itoa(f.Stops) + ` stop(s)` + dist + `</div>`)
	sb.WriteString(`<button class="btn" hx-post="/book" hx-vals='{"flight_id":"` + f.ID + `"}' hx-target="#bookings" hx-swap="innerHTML">Book</button>`)
	sb.WriteString(`</div></div>`)
}

func (s *Store) htmxBook(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id := r.FormValue("flight_id")
	f := s.findFlight(id)
	if f == nil {
		_, _ = w.Write([]byte(`<p class="muted">Flight not found.</p>`))
		return
	}
	s.mu.Lock()
	bid := s.newID("FL")
	b := Booking{BookingID: bid, TripID: "TRIP-001", Status: "CONFIRMED", Flight: *f,
		Travelers: 2, Price: f.Price, Currency: f.Currency, CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	s.bookings[bid] = b
	s.mu.Unlock()
	s.renderBookings(w)
}

func (s *Store) htmxCancel(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	s.mu.Lock()
	if b, ok := s.bookings[r.FormValue("booking_id")]; ok {
		b.Status = "CANCELLED"
		s.bookings[b.BookingID] = b
	}
	s.mu.Unlock()
	s.renderBookings(w)
}

func (s *Store) htmxBookings(w http.ResponseWriter, r *http.Request) { s.renderBookings(w) }

func (s *Store) renderBookings(w http.ResponseWriter) {
	s.mu.Lock()
	bs := make([]Booking, 0, len(s.bookings))
	for _, b := range s.bookings {
		bs = append(bs, b)
	}
	s.mu.Unlock()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if len(bs) == 0 {
		_, _ = w.Write([]byte(`<p class="muted">No bookings yet.</p>`))
		return
	}
	var sb strings.Builder
	for _, b := range bs {
		sb.WriteString(`<div class="card">`)
		sb.WriteString(`<div class="card-top"><strong>` + b.Flight.Airline + `</strong> <span class="badge">` + b.Status + `</span><span class="price">$` + strconv.Itoa(b.Price) + `</span></div>`)
		sb.WriteString(`<div class="row muted">` + b.BookingID + ` · ` + b.Flight.Origin + ` → ` + b.Flight.Destination + `</div>`)
		if b.Status == "CONFIRMED" {
			sb.WriteString(`<button class="btn btn-ghost" hx-post="/cancel" hx-vals='{"booking_id":"` + b.BookingID + `"}' hx-target="#bookings" hx-swap="innerHTML">Cancel</button>`)
		}
		sb.WriteString(`</div>`)
	}
	_, _ = w.Write([]byte(sb.String()))
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func withJSON(h func(w http.ResponseWriter, r *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		h(w, r)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func splitOrigins(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
