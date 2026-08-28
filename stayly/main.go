// Stayly — a standalone WebMCP travel provider (accommodation).
// Human UI (HTMX) and agent mode (WebMCP) share one in-memory state.
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

type Stay struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Neighborhood  string   `json:"neighborhood"`
	Type          string   `json:"type"`
	Guests        int      `json:"max_guests"`
	PricePerNight int      `json:"price_per_night"`
	Currency      string   `json:"currency"`
	Rating        float64  `json:"rating"`
	Amenities     []string `json:"amenities"`
}

type Reservation struct {
	ReservationID string `json:"reservation_id"`
	TripID        string `json:"trip_id"`
	Status        string `json:"status"`
	Stay          Stay   `json:"stay"`
	CheckIn       string `json:"check_in"`
	CheckOut      string `json:"check_out"`
	Guests        int    `json:"guests"`
	Nights        int    `json:"nights"`
	Price         int    `json:"price"`
	Currency      string `json:"currency"`
	CreatedAt     string `json:"created_at"`
}

var stays = []Stay{
	{ID: "ST-001", Name: "Hotel Lumière", Neighborhood: "Le Marais", Type: "Boutique hotel", Guests: 2,
		PricePerNight: 236, Currency: "USD", Rating: 4.8, Amenities: []string{"wifi", "breakfast", "courtyard"}},
	{ID: "ST-002", Name: "Maison Marais", Neighborhood: "Le Marais", Type: "Boutique hotel", Guests: 2,
		PricePerNight: 264, Currency: "USD", Rating: 4.6, Amenities: []string{"wifi", "spa"}},
	{ID: "ST-003", Name: "Le Petit Jardin", Neighborhood: "Montmartre", Type: "Guesthouse", Guests: 2,
		PricePerNight: 198, Currency: "USD", Rating: 4.5, Amenities: []string{"wifi", "garden"}},
	{ID: "ST-004", Name: "Rivoli Residence", Neighborhood: "1er arrondissement", Type: "Apartment", Guests: 4,
		PricePerNight: 290, Currency: "USD", Rating: 4.7, Amenities: []string{"kitchen", "wifi", "washer"}},
}

type Store struct {
	mu           sync.Mutex
	reservations map[string]Reservation
	live         map[string]Stay
	next         int
}

func newStore() *Store { return &Store{reservations: map[string]Reservation{}, live: map[string]Stay{}, next: 1} }

func (s *Store) newID(prefix string) string {
	s.next++
	return prefix + "-" + strconv.Itoa(s.next)
}

type pageData struct {
	ExposedOriginsJS template.JS
}

func main() {
	port := getenv("PORT", "8082")
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
	mux.HandleFunc("GET /api/images", handleImages)
	mux.HandleFunc("GET /api/places", handlePlaces)

	mux.HandleFunc("POST /api/search_stays", withJSON(store.searchStays))
	mux.HandleFunc("POST /api/get_stay_details", withJSON(store.stayDetails))
	mux.HandleFunc("POST /api/check_room_availability", withJSON(store.checkAvailability))
	mux.HandleFunc("POST /api/reserve_room", withJSON(store.reserveRoom))
	mux.HandleFunc("POST /api/modify_reservation", withJSON(store.modifyReservation))
	mux.HandleFunc("POST /api/cancel_reservation", withJSON(store.cancelReservation))
	mux.HandleFunc("GET /api/state", withJSON(store.state))

	mux.HandleFunc("POST /search", store.htmxSearch)
	mux.HandleFunc("POST /reserve", store.htmxReserve)
	mux.HandleFunc("POST /cancel", store.htmxCancel)
	mux.HandleFunc("GET /reservations", store.htmxReservations)

	log.Printf("Stayly listening on http://localhost:%s (exposing tools to %v)", port, exposed)
	log.Fatal(http.ListenAndServe(":"+port, withHeaders(mux)))
}

func withHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	_ = tpl.Execute(w, pageData{ExposedOriginsJS: template.JS(js)})
}

func serveStatic(w http.ResponseWriter, r *http.Request) {
	http.FileServer(http.FS(staticFS)).ServeHTTP(w, r)
}

// ---------------------------------------------------------------------------

func (s *Store) findStay(id string) *Stay {
	for i := range stays {
		if stays[i].ID == id {
			return &stays[i]
		}
	}
	if st, ok := s.live[id]; ok {
		return &st
	}
	return nil
}

func nightsBetween(checkIn, checkOut string) int {
	ci, err1 := time.Parse("2006-01-02", checkIn)
	co, err2 := time.Parse("2006-01-02", checkOut)
	if err1 != nil || err2 != nil || !co.After(ci) {
		return 5
	}
	return int(co.Sub(ci).Hours() / 24)
}

// result is the searchable shape returned to both humans and agents.
func (st Stay) result(checkIn, checkOut string) map[string]any {
	n := nightsBetween(checkIn, checkOut)
	return map[string]any{
		"id": st.ID, "name": st.Name, "neighborhood": st.Neighborhood, "type": st.Type,
		"rating": st.Rating, "amenities": st.Amenities,
		"price_per_night": st.PricePerNight, "nights": n,
		"price": st.PricePerNight * n, "currency": st.Currency,
		"check_in": checkIn, "check_out": checkOut,
	}
}

func (s *Store) searchStays(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Location string `json:"location"`
		CheckIn  string `json:"check_in"`
		CheckOut string `json:"check_out"`
		Guests   int    `json:"guests"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	if in.CheckIn == "" {
		in.CheckIn = "2026-09-15"
	}
	if in.CheckOut == "" {
		in.CheckOut = "2026-09-20"
	}
	results := []map[string]any{}
	// The mock golden inventory is a Paris demo dataset — only return it for Paris.
	if strings.Contains(strings.ToLower(in.Location), "paris") {
		for _, st := range stays {
			if in.Guests > 0 && st.Guests < in.Guests {
				continue
			}
			results = append(results, st.result(in.CheckIn, in.CheckOut))
		}
	}
	// Enrich with real hotels (Photon / OpenStreetMap).
	if lat, lon, err := GeocodeLocation(in.Location); err == nil {
		for i, p := range LivePlacesNear("hotel", "tourism:hotel", lat, lon) {
			st := Stay{
				ID: "ST-LIVE-" + strconv.Itoa(i+1), Name: p.Name, Neighborhood: in.Location,
				Type: "Hotel", Guests: 2, PricePerNight: 150 + (len(p.Name)%6)*40,
				Currency: "USD", Rating: 4.0 + float64(len(p.Name)%8)/20.0, Amenities: []string{"wifi"},
			}
			s.mu.Lock()
			s.live[st.ID] = st
			s.mu.Unlock()
			res := st.result(in.CheckIn, in.CheckOut)
			res["source"] = "live"
			results = append(results, res)
		}
	}
	// Enrich with live Hotelbeds offers (only when credentials are set).
	if live, err := hotelbedsSearch(in.Location, in.CheckIn, in.CheckOut, in.Guests); err == nil && len(live) > 0 {
		for i := range live {
			s.mu.Lock()
			s.live[live[i].ID] = live[i]
			s.mu.Unlock()
			res := live[i].result(in.CheckIn, in.CheckOut)
			res["source"] = "hotelbeds"
			results = append(results, res)
		}
	}
	writeJSON(w, map[string]any{"stays": results})
}

func (s *Store) stayDetails(w http.ResponseWriter, r *http.Request) {
	var in struct {
		StayID string `json:"stay_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	st := s.findStay(in.StayID)
	if st == nil {
		writeErr(w, 404, "stay not found")
		return
	}
	writeJSON(w, map[string]any{"stay": st})
}

func (s *Store) checkAvailability(w http.ResponseWriter, r *http.Request) {
	var in struct {
		StayID   string `json:"stay_id"`
		CheckIn  string `json:"check_in"`
		CheckOut string `json:"check_out"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	st := s.findStay(in.StayID)
	if st == nil {
		writeErr(w, 404, "stay not found")
		return
	}
	n := nightsBetween(in.CheckIn, in.CheckOut)
	writeJSON(w, map[string]any{"stay_id": st.ID, "available": true, "rooms_left": 3,
		"nights": n, "price": st.PricePerNight * n, "currency": st.Currency})
}

func (s *Store) reserveRoom(w http.ResponseWriter, r *http.Request) {
	var in struct {
		StayID   string `json:"stay_id"`
		CheckIn  string `json:"check_in"`
		CheckOut string `json:"check_out"`
		Guests   int    `json:"guests"`
		TripID   string `json:"trip_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	st := s.findStay(in.StayID)
	if st == nil {
		writeErr(w, 404, "stay not found")
		return
	}
	if in.CheckIn == "" {
		in.CheckIn = "2026-09-15"
	}
	if in.CheckOut == "" {
		in.CheckOut = "2026-09-20"
	}
	if in.Guests <= 0 {
		in.Guests = 2
	}
	if in.TripID == "" {
		in.TripID = "TRIP-001"
	}
	n := nightsBetween(in.CheckIn, in.CheckOut)
	s.mu.Lock()
	id := s.newID("ST")
	res := Reservation{ReservationID: id, TripID: in.TripID, Status: "CONFIRMED", Stay: *st,
		CheckIn: in.CheckIn, CheckOut: in.CheckOut, Guests: in.Guests, Nights: n,
		Price: st.PricePerNight * n, Currency: st.Currency, CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	s.reservations[id] = res
	s.mu.Unlock()
	writeJSON(w, map[string]any{"reservation": res})
}

func (s *Store) modifyReservation(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ReservationID string `json:"reservation_id"`
		CheckIn       string `json:"check_in"`
		CheckOut      string `json:"check_out"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	s.mu.Lock()
	res, ok := s.reservations[in.ReservationID]
	if ok {
		if in.CheckIn != "" {
			res.CheckIn = in.CheckIn
		}
		if in.CheckOut != "" {
			res.CheckOut = in.CheckOut
		}
		res.Nights = nightsBetween(res.CheckIn, res.CheckOut)
		res.Price = res.Stay.PricePerNight * res.Nights
		res.Status = "MODIFIED"
		s.reservations[in.ReservationID] = res
	}
	s.mu.Unlock()
	if !ok {
		writeErr(w, 404, "reservation not found")
		return
	}
	writeJSON(w, map[string]any{"reservation": res})
}

func (s *Store) cancelReservation(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ReservationID string `json:"reservation_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	s.mu.Lock()
	res, ok := s.reservations[in.ReservationID]
	if ok {
		res.Status = "CANCELLED"
		s.reservations[in.ReservationID] = res
	}
	s.mu.Unlock()
	if !ok {
		writeErr(w, 404, "reservation not found")
		return
	}
	writeJSON(w, map[string]any{"reservation": res})
}

func (s *Store) state(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	rs := make([]Reservation, 0, len(s.reservations))
	for _, x := range s.reservations {
		rs = append(rs, x)
	}
	s.mu.Unlock()
	writeJSON(w, map[string]any{"reservations": rs})
}

// ---------------------------------------------------------------------------

func (s *Store) htmxSearch(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	location := r.FormValue("location")
	checkIn := r.FormValue("check_in")
	checkOut := r.FormValue("check_out")
	if checkIn == "" {
		checkIn = "2026-09-15"
	}
	if checkOut == "" {
		checkOut = "2026-09-20"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if location == "" {
		_, _ = w.Write([]byte(`<p class="muted">Search for a destination to see real places to stay.</p>`))
		return
	}
	lat, lon, err := GeocodeLocation(location)
	if err != nil {
		_, _ = w.Write([]byte(`<p class="muted">Couldn't find “` + location + `” — try selecting a suggestion.</p>`))
		return
	}
	var sb strings.Builder
	for i, p := range LivePlacesNear("hotel", "tourism:hotel", lat, lon) {
		st := Stay{ID: "ST-LIVE-" + strconv.Itoa(i+1), Name: p.Name, Neighborhood: location, Type: "Hotel", Guests: 2, PricePerNight: 150 + (len(p.Name)%6)*40, Currency: "USD", Rating: 4.0 + float64(len(p.Name)%8)/20.0, Amenities: []string{"wifi"}}
		s.mu.Lock()
		s.live[st.ID] = st
		s.mu.Unlock()
		s.renderStayCard(&sb, st, "Photon", checkIn, checkOut)
	}
	if live, err := hotelbedsSearch(location, checkIn, checkOut, 2); err == nil {
		for _, st := range live {
			s.mu.Lock()
			s.live[st.ID] = st
			s.mu.Unlock()
			s.renderStayCard(&sb, st, "Hotelbeds", checkIn, checkOut)
		}
	}
	if sb.Len() == 0 {
		_, _ = w.Write([]byte(`<p class="muted">No places found near ` + location + `.</p>`))
		return
	}
	_, _ = w.Write([]byte(sb.String()))
}

func (s *Store) renderStayCard(sb *strings.Builder, st Stay, live, checkIn, checkOut string) {
	badge := ""
	if live != "" {
		badge = `<span class="badge">live · ` + live + `</span>`
	}
	sb.WriteString(`<div class="card" data-q="` + strings.ReplaceAll(st.Name, `"`, "&quot;") + `">`)
	sb.WriteString(`<div class="card-img">…</div>`)
	sb.WriteString(`<div class="card-body">`)
	sb.WriteString(`<div class="card-top"><strong>` + st.Name + `</strong>` + badge + `<span class="price">$` + strconv.Itoa(st.PricePerNight) + `/nt</span></div>`)
	sb.WriteString(`<div class="row muted">` + st.Neighborhood + ` · ` + st.Type + ` · ★ ` + strconv.FormatFloat(st.Rating, 'f', 1, 64) + `</div>`)
	sb.WriteString(`<button class="btn" hx-post="/reserve" hx-vals='{"stay_id":"` + st.ID + `","check_in":"` + checkIn + `","check_out":"` + checkOut + `"}' hx-target="#reservations" hx-swap="innerHTML">Reserve</button>`)
	sb.WriteString(`</div></div>`)
}

func (s *Store) htmxReserve(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	st := s.findStay(r.FormValue("stay_id"))
	if st == nil {
		_, _ = w.Write([]byte(`<p class="muted">Stay not found.</p>`))
		return
	}
	checkIn := r.FormValue("check_in")
	checkOut := r.FormValue("check_out")
	if checkIn == "" {
		checkIn = "2026-09-15"
	}
	if checkOut == "" {
		checkOut = "2026-09-20"
	}
	n := nightsBetween(checkIn, checkOut)
	s.mu.Lock()
	id := s.newID("ST")
	res := Reservation{ReservationID: id, TripID: "TRIP-001", Status: "CONFIRMED", Stay: *st,
		CheckIn: checkIn, CheckOut: checkOut, Guests: 2, Nights: n, Price: st.PricePerNight * n,
		Currency: st.Currency, CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	s.reservations[id] = res
	s.mu.Unlock()
	s.renderReservations(w)
}

func (s *Store) htmxCancel(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	s.mu.Lock()
	if res, ok := s.reservations[r.FormValue("reservation_id")]; ok {
		res.Status = "CANCELLED"
		s.reservations[res.ReservationID] = res
	}
	s.mu.Unlock()
	s.renderReservations(w)
}

func (s *Store) htmxReservations(w http.ResponseWriter, r *http.Request) { s.renderReservations(w) }

func (s *Store) renderReservations(w http.ResponseWriter) {
	s.mu.Lock()
	rs := make([]Reservation, 0, len(s.reservations))
	for _, x := range s.reservations {
		rs = append(rs, x)
	}
	s.mu.Unlock()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if len(rs) == 0 {
		_, _ = w.Write([]byte(`<p class="muted">No reservations yet.</p>`))
		return
	}
	var sb strings.Builder
	for _, x := range rs {
		sb.WriteString(`<div class="card">`)
		sb.WriteString(`<div class="card-top"><strong>` + x.Stay.Name + `</strong> <span class="badge">` + x.Status + `</span><span class="price">$` + strconv.Itoa(x.Price) + `</span></div>`)
		sb.WriteString(`<div class="row muted">` + x.ReservationID + ` · ` + x.CheckIn + ` → ` + x.CheckOut + ` · ` + strconv.Itoa(x.Nights) + ` nights</div>`)
		if x.Status == "CONFIRMED" {
			sb.WriteString(`<button class="btn btn-ghost" hx-post="/cancel" hx-vals='{"reservation_id":"` + x.ReservationID + `"}' hx-target="#reservations" hx-swap="innerHTML">Cancel</button>`)
		}
		sb.WriteString(`</div>`)
	}
	_, _ = w.Write([]byte(sb.String()))
}

// ---------------------------------------------------------------------------

func withJSON(h func(w http.ResponseWriter, r *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		h(w, r)
	}
}

func writeJSON(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }

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
