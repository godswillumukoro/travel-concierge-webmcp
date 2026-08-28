// Explore — a standalone WebMCP travel provider (activities & experiences).
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

type Experience struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Category     string  `json:"category"`
	PricePerPax  int     `json:"price_per_person"`
	Currency     string  `json:"currency"`
	Duration     string  `json:"duration"`
	Rating       float64 `json:"rating"`
}

type Booking struct {
	BookingID  string     `json:"booking_id"`
	TripID     string     `json:"trip_id"`
	Status     string     `json:"status"`
	Experience Experience `json:"experience"`
	Date       string     `json:"date"`
	Travelers  int        `json:"travelers"`
	Price      int        `json:"price"`
	Currency   string     `json:"currency"`
	CreatedAt  string     `json:"created_at"`
}

var experiences = []Experience{
	{ID: "EX-001", Name: "Seine Evening Cruise", Category: "Cruise", PricePerPax: 48, Currency: "USD", Duration: "2h", Rating: 4.9},
	{ID: "EX-002", Name: "Louvre Guided Tour", Category: "Museum", PricePerPax: 65, Currency: "USD", Duration: "3h", Rating: 4.8},
	{ID: "EX-003", Name: "Montmartre Food Walk", Category: "Food tour", PricePerPax: 44, Currency: "USD", Duration: "3h", Rating: 4.7},
	{ID: "EX-004", Name: "Versailles Day Trip", Category: "Day trip", PricePerPax: 105, Currency: "USD", Duration: "8h", Rating: 4.6},
}

type Store struct {
	mu       sync.Mutex
	bookings map[string]Booking
	live     map[string]Experience
	next     int
}

func newStore() *Store { return &Store{bookings: map[string]Booking{}, live: map[string]Experience{}, next: 1} }

func (s *Store) newID(prefix string) string {
	s.next++
	return prefix + "-" + strconv.Itoa(s.next)
}

type pageData struct{ ExposedOriginsJS template.JS }

func main() {
	port := getenv("PORT", "8084")
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

	mux.HandleFunc("POST /api/search_experiences", withJSON(store.searchExperiences))
	mux.HandleFunc("POST /api/get_experience_details", withJSON(store.details))
	mux.HandleFunc("POST /api/check_experience_availability", withJSON(store.checkAvailability))
	mux.HandleFunc("POST /api/book_experience", withJSON(store.bookExperience))
	mux.HandleFunc("POST /api/cancel_booking", withJSON(store.cancel))
	mux.HandleFunc("GET /api/state", withJSON(store.state))

	mux.HandleFunc("POST /search", store.htmxSearch)
	mux.HandleFunc("POST /book", store.htmxBook)
	mux.HandleFunc("POST /cancel", store.htmxCancel)
	mux.HandleFunc("GET /bookings", store.htmxBookings)

	log.Printf("Explore listening on http://localhost:%s (exposing tools to %v)", port, exposed)
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

func (s *Store) findExp(id string) *Experience {
	for i := range experiences {
		if experiences[i].ID == id {
			return &experiences[i]
		}
	}
	if e, ok := s.live[id]; ok {
		return &e
	}
	return nil
}

func (e Experience) result(travelers int) map[string]any {
	if travelers <= 0 {
		travelers = 2
	}
	return map[string]any{
		"id": e.ID, "name": e.Name, "category": e.Category, "duration": e.Duration,
		"rating": e.Rating, "price_per_person": e.PricePerPax, "travelers": travelers,
		"price": e.PricePerPax * travelers, "currency": e.Currency,
	}
}

func (s *Store) searchExperiences(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Location  string `json:"location"`
		Date      string `json:"date"`
		Travelers int    `json:"travelers"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	if in.Travelers <= 0 {
		in.Travelers = 2
	}
	results := []map[string]any{}
	// The mock golden inventory is a Paris demo dataset — only return it for Paris.
	if strings.Contains(strings.ToLower(in.Location), "paris") {
		for _, e := range experiences {
			results = append(results, e.result(in.Travelers))
		}
	}
	// Enrich with real attractions (Photon / OpenStreetMap).
	if lat, lon, err := GeocodeLocation(in.Location); err == nil {
		for i, p := range LivePlacesNear("museum", "tourism:museum", lat, lon) {
			pp := 35 + (len(p.Name)%4)*15
			e := Experience{
				ID: "EX-LIVE-" + strconv.Itoa(i+1), Name: p.Name, Category: "Museum",
				PricePerPax: pp, Currency: "USD", Duration: "2h",
				Rating: 4.2 + float64(len(p.Name)%8)/20.0,
			}
			s.mu.Lock()
			s.live[e.ID] = e
			s.mu.Unlock()
			res := e.result(in.Travelers)
			res["source"] = "live"
			results = append(results, res)
		}
	}
	writeJSON(w, map[string]any{"experiences": results})
}

func (s *Store) details(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ExperienceID string `json:"experience_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	e := s.findExp(in.ExperienceID)
	if e == nil {
		writeErr(w, 404, "experience not found")
		return
	}
	writeJSON(w, map[string]any{"experience": e})
}

func (s *Store) checkAvailability(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ExperienceID string `json:"experience_id"`
		Date         string `json:"date"`
		Travelers    int    `json:"travelers"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	e := s.findExp(in.ExperienceID)
	if e == nil {
		writeErr(w, 404, "experience not found")
		return
	}
	if in.Travelers <= 0 {
		in.Travelers = 2
	}
	writeJSON(w, map[string]any{"experience_id": e.ID, "available": true, "spots_left": 6,
		"price": e.PricePerPax * in.Travelers, "currency": e.Currency})
}

func (s *Store) bookExperience(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ExperienceID string `json:"experience_id"`
		Date         string `json:"date"`
		Travelers    int    `json:"travelers"`
		TripID       string `json:"trip_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	e := s.findExp(in.ExperienceID)
	if e == nil {
		writeErr(w, 404, "experience not found")
		return
	}
	if in.Date == "" {
		in.Date = "2026-09-17"
	}
	if in.Travelers <= 0 {
		in.Travelers = 2
	}
	if in.TripID == "" {
		in.TripID = "TRIP-001"
	}
	s.mu.Lock()
	id := s.newID("EX")
	b := Booking{BookingID: id, TripID: in.TripID, Status: "CONFIRMED", Experience: *e,
		Date: in.Date, Travelers: in.Travelers, Price: e.PricePerPax * in.Travelers,
		Currency: e.Currency, CreatedAt: time.Now().UTC().Format(time.RFC3339)}
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
		_, _ = w.Write([]byte(`<p class="muted">Search for a destination to see real experiences.</p>`))
		return
	}
	lat, lon, err := GeocodeLocation(location)
	if err != nil {
		_, _ = w.Write([]byte(`<p class="muted">Couldn't find “` + location + `” — try selecting a suggestion.</p>`))
		return
	}
	places := LivePlacesNear("museum", "tourism:museum", lat, lon)
	if len(places) == 0 {
		_, _ = w.Write([]byte(`<p class="muted">No experiences found near ` + location + `.</p>`))
		return
	}
	var sb strings.Builder
	for i, p := range places {
		pp := 35 + (len(p.Name)%4)*15
		e := Experience{ID: "EX-LIVE-" + strconv.Itoa(i+1), Name: p.Name, Category: "Museum", PricePerPax: pp, Currency: "USD", Duration: "2h", Rating: 4.2 + float64(len(p.Name)%8)/20.0}
		s.mu.Lock()
		s.live[e.ID] = e
		s.mu.Unlock()
		s.renderExperienceCard(&sb, e, "Photon")
	}
	_, _ = w.Write([]byte(sb.String()))
}

func (s *Store) renderExperienceCard(sb *strings.Builder, e Experience, live string) {
	badge := ""
	if live != "" {
		badge = `<span class="badge">live · ` + live + `</span>`
	}
	sb.WriteString(`<div class="card" data-q="` + strings.ReplaceAll(e.Name, `"`, "&quot;") + `">`)
	sb.WriteString(`<div class="card-img">…</div>`)
	sb.WriteString(`<div class="card-body">`)
	sb.WriteString(`<div class="card-top"><strong>` + e.Name + `</strong>` + badge + `<span class="price">$` + strconv.Itoa(e.PricePerPax) + `/pp</span></div>`)
	sb.WriteString(`<div class="row muted">` + e.Category + ` · ` + e.Duration + ` · ★ ` + strconv.FormatFloat(e.Rating, 'f', 1, 64) + `</div>`)
	sb.WriteString(`<button class="btn" hx-post="/book" hx-vals='{"experience_id":"` + e.ID + `"}' hx-target="#bookings" hx-swap="innerHTML">Book</button>`)
	sb.WriteString(`</div></div>`)
}

func (s *Store) htmxBook(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	e := s.findExp(r.FormValue("experience_id"))
	if e == nil {
		_, _ = w.Write([]byte(`<p class="muted">Experience not found.</p>`))
		return
	}
	s.mu.Lock()
	id := s.newID("EX")
	b := Booking{BookingID: id, TripID: "TRIP-001", Status: "CONFIRMED", Experience: *e,
		Date: "2026-09-17", Travelers: 2, Price: e.PricePerPax * 2, Currency: e.Currency,
		CreatedAt: time.Now().UTC().Format(time.RFC3339)}
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
		_, _ = w.Write([]byte(`<p class="muted">No bookings yet.</p>`))
		return
	}
	var sb strings.Builder
	for _, b := range bs {
		sb.WriteString(`<div class="card">`)
		sb.WriteString(`<div class="card-top"><strong>` + b.Experience.Name + `</strong> <span class="badge">` + b.Status + `</span><span class="price">$` + strconv.Itoa(b.Price) + `</span></div>`)
		sb.WriteString(`<div class="row muted">` + b.BookingID + ` · ` + b.Date + ` · ` + strconv.Itoa(b.Travelers) + ` travelers</div>`)
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
