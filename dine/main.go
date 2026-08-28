// Dine — a standalone WebMCP travel provider (restaurant discovery & booking).
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

type Restaurant struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Cuisine     string  `json:"cuisine"`
	Neighborhood string `json:"neighborhood"`
	PricePerPax int     `json:"price_per_person"`
	Currency    string  `json:"currency"`
	Rating      float64 `json:"rating"`
}

type Booking struct {
	BookingID   string     `json:"booking_id"`
	TripID      string     `json:"trip_id"`
	Status      string     `json:"status"`
	Restaurant  Restaurant `json:"restaurant"`
	Date        string     `json:"date"`
	Time        string     `json:"time"`
	PartySize   int        `json:"party_size"`
	Price       int        `json:"price"`
	Currency    string     `json:"currency"`
	CreatedAt   string     `json:"created_at"`
}

var restaurants = []Restaurant{
	{ID: "DN-001", Name: "Le Jardin", Cuisine: "Modern French", Neighborhood: "Saint-Germain", PricePerPax: 60, Currency: "USD", Rating: 4.9},
	{ID: "DN-002", Name: "Bistro Victor", Cuisine: "Bistro", Neighborhood: "Le Marais", PricePerPax: 47, Currency: "USD", Rating: 4.4},
	{ID: "DN-003", Name: "L'Étoile", Cuisine: "Fine dining", Neighborhood: "8e arrondissement", PricePerPax: 90, Currency: "USD", Rating: 4.8},
	{ID: "DN-004", Name: "Chez Marcel", Cuisine: "Brasserie", Neighborhood: "Montmartre", PricePerPax: 70, Currency: "USD", Rating: 4.5},
}

type Store struct {
	mu       sync.Mutex
	bookings map[string]Booking
	live     map[string]Restaurant
	next     int
}

func newStore() *Store { return &Store{bookings: map[string]Booking{}, live: map[string]Restaurant{}, next: 1} }

func (s *Store) newID(prefix string) string {
	s.next++
	return prefix + "-" + strconv.Itoa(s.next)
}

type pageData struct{ ExposedOriginsJS template.JS }

func main() {
	port := getenv("PORT", "8083")
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

	mux.HandleFunc("POST /api/search_restaurants", withJSON(store.searchRestaurants))
	mux.HandleFunc("POST /api/get_restaurant_details", withJSON(store.details))
	mux.HandleFunc("POST /api/check_table_availability", withJSON(store.checkAvailability))
	mux.HandleFunc("POST /api/book_table", withJSON(store.bookTable))
	mux.HandleFunc("POST /api/cancel_reservation", withJSON(store.cancel))
	mux.HandleFunc("GET /api/state", withJSON(store.state))

	mux.HandleFunc("POST /search", store.htmxSearch)
	mux.HandleFunc("POST /book", store.htmxBook)
	mux.HandleFunc("POST /cancel", store.htmxCancel)
	mux.HandleFunc("GET /bookings", store.htmxBookings)

	log.Printf("Dine listening on http://localhost:%s (exposing tools to %v)", port, exposed)
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

func (s *Store) findRestaurant(id string) *Restaurant {
	for i := range restaurants {
		if restaurants[i].ID == id {
			return &restaurants[i]
		}
	}
	if r, ok := s.live[id]; ok {
		return &r
	}
	return nil
}

func (r Restaurant) result(party int) map[string]any {
	if party <= 0 {
		party = 2
	}
	return map[string]any{
		"id": r.ID, "name": r.Name, "cuisine": r.Cuisine, "neighborhood": r.Neighborhood,
		"rating": r.Rating, "price_per_person": r.PricePerPax, "party_size": party,
		"price": r.PricePerPax * party, "currency": r.Currency,
	}
}

func (s *Store) searchRestaurants(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Location  string `json:"location"`
		Date      string `json:"date"`
		PartySize int    `json:"party_size"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	if in.PartySize <= 0 {
		in.PartySize = 2
	}
	results := []map[string]any{}
	// The mock golden inventory is a Paris demo dataset — only return it for Paris.
	if strings.Contains(strings.ToLower(in.Location), "paris") {
		for _, rest := range restaurants {
			results = append(results, rest.result(in.PartySize))
		}
	}
	// Enrich with real restaurants (Photon / OpenStreetMap).
	if lat, lon, err := GeocodeLocation(in.Location); err == nil {
		for i, p := range LivePlacesNear("restaurant", "amenity:restaurant", lat, lon) {
			pp := 30 + (len(p.Name)%6)*12
			r := Restaurant{
				ID: "DN-LIVE-" + strconv.Itoa(i+1), Name: p.Name, Cuisine: "Local",
				Neighborhood: in.Location, PricePerPax: pp, Currency: "USD",
				Rating: 4.0 + float64(len(p.Name)%10)/20.0,
			}
			s.mu.Lock()
			s.live[r.ID] = r
			s.mu.Unlock()
			res := r.result(in.PartySize)
			res["source"] = "live"
			results = append(results, res)
		}
	}
	writeJSON(w, map[string]any{"restaurants": results})
}

func (s *Store) details(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RestaurantID string `json:"restaurant_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	rest := s.findRestaurant(in.RestaurantID)
	if rest == nil {
		writeErr(w, 404, "restaurant not found")
		return
	}
	writeJSON(w, map[string]any{"restaurant": rest})
}

func (s *Store) checkAvailability(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RestaurantID string `json:"restaurant_id"`
		Date         string `json:"date"`
		Time         string `json:"time"`
		PartySize    int    `json:"party_size"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	rest := s.findRestaurant(in.RestaurantID)
	if rest == nil {
		writeErr(w, 404, "restaurant not found")
		return
	}
	if in.PartySize <= 0 {
		in.PartySize = 2
	}
	writeJSON(w, map[string]any{"restaurant_id": rest.ID, "available": true, "tables_left": 4,
		"price": rest.PricePerPax * in.PartySize, "currency": rest.Currency})
}

func (s *Store) bookTable(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RestaurantID string `json:"restaurant_id"`
		Date         string `json:"date"`
		Time         string `json:"time"`
		PartySize    int    `json:"party_size"`
		TripID       string `json:"trip_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	rest := s.findRestaurant(in.RestaurantID)
	if rest == nil {
		writeErr(w, 404, "restaurant not found")
		return
	}
	if in.Date == "" {
		in.Date = "2026-09-16"
	}
	if in.Time == "" {
		in.Time = "19:30"
	}
	if in.PartySize <= 0 {
		in.PartySize = 2
	}
	if in.TripID == "" {
		in.TripID = "TRIP-001"
	}
	s.mu.Lock()
	id := s.newID("DN")
	b := Booking{BookingID: id, TripID: in.TripID, Status: "CONFIRMED", Restaurant: *rest,
		Date: in.Date, Time: in.Time, PartySize: in.PartySize, Price: rest.PricePerPax * in.PartySize,
		Currency: rest.Currency, CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	s.bookings[id] = b
	s.mu.Unlock()
	writeJSON(w, map[string]any{"booking": b})
}

func (s *Store) cancel(w http.ResponseWriter, r *http.Request) {
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
	writeJSON(w, map[string]any{"bookings": bs})
}

// ---------------------------------------------------------------------------

func (s *Store) htmxSearch(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	location := r.FormValue("location")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if location == "" {
		_, _ = w.Write([]byte(`<p class="muted">Search for a destination to see real places to eat.</p>`))
		return
	}
	lat, lon, err := GeocodeLocation(location)
	if err != nil {
		_, _ = w.Write([]byte(`<p class="muted">Couldn't find “` + location + `” — try selecting a suggestion.</p>`))
		return
	}
	places := LivePlacesNear("restaurant", "amenity:restaurant", lat, lon)
	if len(places) == 0 {
		_, _ = w.Write([]byte(`<p class="muted">No restaurants found near ` + location + `.</p>`))
		return
	}
	var sb strings.Builder
	for i, p := range places {
		pp := 30 + (len(p.Name)%6)*12
		rest := Restaurant{ID: "DN-LIVE-" + strconv.Itoa(i+1), Name: p.Name, Cuisine: "Local", Neighborhood: location, PricePerPax: pp, Currency: "USD", Rating: 4.0 + float64(len(p.Name)%10)/20.0}
		s.mu.Lock()
		s.live[rest.ID] = rest
		s.mu.Unlock()
		s.renderRestaurantCard(&sb, rest, "Photon")
	}
	_, _ = w.Write([]byte(sb.String()))
}

func (s *Store) renderRestaurantCard(sb *strings.Builder, rest Restaurant, live string) {
	badge := ""
	if live != "" {
		badge = `<span class="badge">live · ` + live + `</span>`
	}
	sb.WriteString(`<div class="card" data-q="` + strings.ReplaceAll(rest.Name, `"`, "&quot;") + `">`)
	sb.WriteString(`<div class="card-img">…</div>`)
	sb.WriteString(`<div class="card-body">`)
	sb.WriteString(`<div class="card-top"><strong>` + rest.Name + `</strong>` + badge + `<span class="price">$` + strconv.Itoa(rest.PricePerPax) + `/pp</span></div>`)
	sb.WriteString(`<div class="row muted">` + rest.Cuisine + ` · ` + rest.Neighborhood + ` · ★ ` + strconv.FormatFloat(rest.Rating, 'f', 1, 64) + `</div>`)
	sb.WriteString(`<button class="btn" hx-post="/book" hx-vals='{"restaurant_id":"` + rest.ID + `"}' hx-target="#bookings" hx-swap="innerHTML">Book 7:30 PM</button>`)
	sb.WriteString(`</div></div>`)
}

func (s *Store) htmxBook(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	rest := s.findRestaurant(r.FormValue("restaurant_id"))
	if rest == nil {
		_, _ = w.Write([]byte(`<p class="muted">Restaurant not found.</p>`))
		return
	}
	s.mu.Lock()
	id := s.newID("DN")
	b := Booking{BookingID: id, TripID: "TRIP-001", Status: "CONFIRMED", Restaurant: *rest,
		Date: "2026-09-16", Time: "19:30", PartySize: 2, Price: rest.PricePerPax * 2,
		Currency: rest.Currency, CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	s.bookings[id] = b
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
		_, _ = w.Write([]byte(`<p class="muted">No reservations yet.</p>`))
		return
	}
	var sb strings.Builder
	for _, b := range bs {
		sb.WriteString(`<div class="card">`)
		sb.WriteString(`<div class="card-top"><strong>` + b.Restaurant.Name + `</strong> <span class="badge">` + b.Status + `</span><span class="price">$` + strconv.Itoa(b.Price) + `</span></div>`)
		sb.WriteString(`<div class="row muted">` + b.BookingID + ` · ` + b.Date + ` ` + b.Time + ` · ` + strconv.Itoa(b.PartySize) + ` guests</div>`)
		if b.Status == "CONFIRMED" {
			sb.WriteString(`<button class="btn btn-ghost" hx-post="/cancel" hx-vals='{"booking_id":"` + b.BookingID + `"}' hx-target="#bookings" hx-swap="innerHTML">Cancel</button>`)
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
