// Ride — a standalone WebMCP travel provider (transportation).
//
// Ride is "real-feeling": pickup/dropoff autocomplete and route distance/duration
// come from free, keyless APIs (Photon geocoding + OSRM routing). The fare is a
// deterministic function of the real route; booking is mocked (no payment).
//
// Human UI (HTMX) and agent mode (WebMCP) share one in-memory state.
package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed static
var staticFS embed.FS

// ---------------------------------------------------------------------------
// Vehicles & fares (mock pricing on real routes)
// ---------------------------------------------------------------------------

type vehicle struct {
	ID, Type, Vehicle     string
	Base, PerKm, PerMin   float64
	Capacity              int
}

var vehicles = []vehicle{
	{ID: "RD-001", Type: "Private transfer", Vehicle: "Sedan", Base: 2.0, PerKm: 0.55, PerMin: 0.30, Capacity: 3},
	{ID: "RD-002", Type: "Ride-share", Vehicle: "Compact", Base: 1.2, PerKm: 0.35, PerMin: 0.18, Capacity: 2},
	{ID: "RD-003", Type: "Premium sedan", Vehicle: "Mercedes E-Class", Base: 3.0, PerKm: 0.80, PerMin: 0.45, Capacity: 3},
}

func fare(v vehicle, distKm, durMin float64) int {
	return int(math.Round(v.Base + v.PerKm*distKm + v.PerMin*durMin))
}

// ---------------------------------------------------------------------------
// Real-data clients (free, keyless). Each has a cache + graceful fallback so
// the demo never breaks when offline.
// ---------------------------------------------------------------------------

type place struct {
	Name    string  `json:"name"`
	Address string  `json:"address"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
}

var (
	geoMu     sync.Mutex
	placeCache = map[string][]place{}
	routeCache = map[string][2]float64{} // "lat,lon;lat,lon" -> distM, durS
)

// Known-place gazetteer: deterministic fallback for geocoding (also powers the
// offline golden trip). Real places resolve through Photon first when possible.
var gazetteer = map[string][2]float64{ // "lat,lon"
	"cdg":              {49.0097, 2.5479},
	"charles de gaulle": {49.0097, 2.5479},
	"paris cdg":         {49.0097, 2.5479},
	"los":               {6.5774, 3.3212},
	"murtala muhammed":  {6.5774, 3.3212},
	"lagos airport":     {6.5774, 3.3212},
	"paris":             {48.8566, 2.3522},
	"lagos":             {6.5244, 3.3792},
	"accra":             {5.6037, -0.1870},
	"victoria island":   {6.4281, 3.4160},
	"osapa":             {6.4398, 3.5126},
	"lekki":             {6.4470, 3.5140},
	"ikoyi":             {6.4500, 3.4400},
}

type photonResp struct {
	Features []struct {
		Properties struct {
			Name, Street, Housenumber, Postcode, City, State, Country string
		} `json:"properties"`
		Geometry struct {
			Coordinates []float64 `json:"coordinates"` // [lon, lat]
		} `json:"geometry"`
	} `json:"features"`
}

type osrmResp struct {
	Routes []struct {
		Distance float64 `json:"distance"`
		Duration float64 `json:"duration"`
	} `json:"routes"`
}

var httpClient = &http.Client{Timeout: 8 * time.Second}

const userAgent = "concierge-demo/1.0 (contact: demo@example.com)"

func photonSearch(q string) ([]place, error) {
	geoMu.Lock()
	if c, ok := placeCache[q]; ok {
		geoMu.Unlock()
		return c, nil
	}
	geoMu.Unlock()

	u := "https://photon.komoot.io/api/?q=" + url.QueryEscape(q) + "&limit=6&lang=en"
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", userAgent)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("photon status %d", resp.StatusCode)
	}
	var pr photonResp
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return nil, err
	}
	out := make([]place, 0, len(pr.Features))
	for _, f := range pr.Features {
		if len(f.Geometry.Coordinates) < 2 {
			continue
		}
		p := place{
			Name: f.Properties.Name,
			Lat:  f.Geometry.Coordinates[1],
			Lon:  f.Geometry.Coordinates[0],
		}
		parts := []string{}
		if f.Properties.Street != "" {
			s := f.Properties.Street
			if f.Properties.Housenumber != "" {
				s += " " + f.Properties.Housenumber
			}
			parts = append(parts, s)
		}
		for _, c := range []string{f.Properties.Postcode, f.Properties.City, f.Properties.State, f.Properties.Country} {
			if c != "" && !contains(parts, c) {
				parts = append(parts, c)
			}
		}
		p.Address = strings.Join(parts, ", ")
		if p.Name == "" {
			p.Name = p.Address
		}
		out = append(out, p)
	}

	geoMu.Lock()
	placeCache[q] = out
	geoMu.Unlock()
	return out, nil
}

// gazetteerKeys is the gazetteer sorted by key length (longest first) so the
// offline substring fallback is deterministic.
var gazetteerKeys = func() []string {
	ks := make([]string, 0, len(gazetteer))
	for k := range gazetteer {
		ks = append(ks, k)
	}
	sort.Slice(ks, func(i, j int) bool {
		if len(ks[i]) != len(ks[j]) {
			return len(ks[i]) > len(ks[j])
		}
		return ks[i] < ks[j]
	})
	return ks
}()

// resolvePlace turns free text (or "lat,lon") into coordinates.
// Resolution order: explicit coords -> exact gazetteer hit -> comma-token hit ->
// Photon (online) -> longest-substring gazetteer fallback (offline).
func resolvePlace(name string) (place, error) {
	name = strings.TrimSpace(name)
	if parts := strings.Split(name, ","); len(parts) == 2 {
		lat, err1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
		lon, err2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		if err1 == nil && err2 == nil {
			return place{Name: name, Lat: lat, Lon: lon}, nil
		}
	}
	if c, ok := gazetteer[strings.ToLower(name)]; ok {
		return place{Name: name, Lat: c[0], Lon: c[1]}, nil
	}
	for _, tok := range strings.Split(name, ",") {
		if c, ok := gazetteer[strings.ToLower(strings.TrimSpace(tok))]; ok {
			return place{Name: name, Lat: c[0], Lon: c[1]}, nil
		}
	}
	if results, err := photonSearch(name); err == nil && len(results) > 0 {
		return results[0], nil
	}
	for _, k := range gazetteerKeys {
		if strings.Contains(strings.ToLower(name), k) {
			c := gazetteer[k]
			return place{Name: name, Lat: c[0], Lon: c[1]}, nil
		}
	}
	return place{}, fmt.Errorf("could not resolve place: %q", name)
}

func osrmRoute(a, b place) (distM, durS float64, err error) {
	key := fmt.Sprintf("%.5f,%.5f;%.5f,%.5f", a.Lat, a.Lon, b.Lat, b.Lon)
	geoMu.Lock()
	if c, ok := routeCache[key]; ok {
		geoMu.Unlock()
		return c[0], c[1], nil
	}
	geoMu.Unlock()

	u := fmt.Sprintf("https://router.project-osrm.org/route/v1/driving/%.5f,%.5f;%.5f,%.5f?overview=false",
		a.Lon, a.Lat, b.Lon, b.Lat)
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", userAgent)
	resp, err := httpClient.Do(req)
	if err != nil {
		return greatCircle(a, b)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return greatCircle(a, b)
	}
	var or osrmResp
	if err := json.NewDecoder(resp.Body).Decode(&or); err != nil || len(or.Routes) == 0 {
		return greatCircle(a, b)
	}
	distM, durS = or.Routes[0].Distance, or.Routes[0].Duration

	geoMu.Lock()
	routeCache[key] = [2]float64{distM, durS}
	geoMu.Unlock()
	return distM, durS, nil
}

// greatCircle is the offline fallback: haversine distance, ~40 km/h city driving.
func greatCircle(a, b place) (float64, float64, error) {
	const R = 6371.0
	la1, lo1 := a.Lat*math.Pi/180, a.Lon*math.Pi/180
	la2, lo2 := b.Lat*math.Pi/180, b.Lon*math.Pi/180
	dLat, dLon := la2-la1, lo2-lo1
	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(la1)*math.Cos(la2)*math.Sin(dLon/2)*math.Sin(dLon/2)
	distKm := 2 * R * math.Asin(math.Sqrt(h))
	durMin := distKm / 40.0 * 60.0
	return distKm * 1000, durMin * 60, nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Bookings
// ---------------------------------------------------------------------------

type Ride struct {
	ID          string  `json:"id"`
	Type        string  `json:"type"`
	Vehicle     string  `json:"vehicle"`
	Price       int     `json:"price"`
	Currency    string  `json:"currency"`
	Capacity    int     `json:"capacity"`
	ETA         string  `json:"eta"`
	DistanceKm  float64 `json:"distance_km"`
	DurationMin float64 `json:"duration_min"`
}

type Booking struct {
	BookingID  string  `json:"booking_id"`
	TripID     string  `json:"trip_id"`
	Status     string  `json:"status"`
	Ride       Ride    `json:"ride"`
	Pickup     string  `json:"pickup"`
	Dropoff    string  `json:"dropoff"`
	DateTime   string  `json:"datetime"`
	Passengers int     `json:"passengers"`
	Price      int     `json:"price"`
	Currency   string  `json:"currency"`
	DistanceKm float64 `json:"distance_km"`
	DurationMin float64 `json:"duration_min"`
	CreatedAt  string  `json:"created_at"`
}

type Store struct {
	mu       sync.Mutex
	bookings map[string]Booking
	next     int
}

func newStore() *Store { return &Store{bookings: map[string]Booking{}, next: 1} }

func (s *Store) newID(prefix string) string {
	s.next++
	return prefix + "-" + strconv.Itoa(s.next)
}

type pageData struct{ ExposedOriginsJS template.JS }

func main() {
	port := getenv("PORT", "8085")
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

	mux.HandleFunc("GET /api/places", withJSON(store.handlePlaces))
	mux.HandleFunc("POST /api/search_rides", withJSON(store.searchRides))
	mux.HandleFunc("POST /api/get_ride_quote", withJSON(store.quote))
	mux.HandleFunc("POST /api/book_ride", withJSON(store.bookRide))
	mux.HandleFunc("POST /api/modify_ride", withJSON(store.modifyRide))
	mux.HandleFunc("POST /api/cancel_ride", withJSON(store.cancelRide))
	mux.HandleFunc("GET /api/state", withJSON(store.state))

	mux.HandleFunc("POST /search", store.htmxSearch)
	mux.HandleFunc("POST /book", store.htmxBook)
	mux.HandleFunc("POST /cancel", store.htmxCancel)
	mux.HandleFunc("GET /bookings", store.htmxBookings)

	log.Printf("Ride listening on http://localhost:%s (exposing tools to %v)", port, exposed)
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
// Places autocomplete (free: Photon)
// ---------------------------------------------------------------------------

func (s *Store) handlePlaces(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if strings.TrimSpace(q) == "" {
		writeJSON(w, map[string]any{"places": []place{}})
		return
	}
	results, err := photonSearch(q)
	if err != nil {
		writeJSON(w, map[string]any{"places": []place{}})
		return
	}
	writeJSON(w, map[string]any{"places": results})
}

// ---------------------------------------------------------------------------
// Search / quote / book
// ---------------------------------------------------------------------------

func (s *Store) searchRides(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Pickup   string  `json:"pickup"`
		Dropoff  string  `json:"dropoff"`
		DateTime string  `json:"datetime"`
		Passengers int   `json:"passengers"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	if in.Pickup == "" {
		in.Pickup = "CDG"
	}
	if in.Dropoff == "" {
		in.Dropoff = "Hotel Lumière"
	}
	if in.Passengers <= 0 {
		in.Passengers = 2
	}

	from, err1 := resolvePlace(in.Pickup)
	to, err2 := resolvePlace(in.Dropoff)
	if err1 != nil || err2 != nil {
		writeErr(w, 400, "could not resolve one of the locations")
		return
	}
	distM, durS, _ := osrmRoute(from, to)
	distKm, durMin := distM/1000.0, durS/60.0

	results := []Ride{}
	for _, v := range vehicles {
		if in.Passengers > v.Capacity {
			continue
		}
		results = append(results, Ride{
			ID: v.ID, Type: v.Type, Vehicle: v.Vehicle, Capacity: v.Capacity,
			Price: fare(v, distKm, durMin), Currency: "USD",
			ETA: fmt.Sprintf("%d min", int(math.Round(durMin))),
			DistanceKm: math.Round(distKm*10) / 10, DurationMin: math.Round(durMin*10) / 10,
		})
	}
	writeJSON(w, map[string]any{
		"rides": results,
		"route": map[string]any{
			"pickup": in.Pickup, "dropoff": in.Dropoff,
			"pickup_coords":  []float64{from.Lat, from.Lon},
			"dropoff_coords": []float64{to.Lat, to.Lon},
			"distance_km": math.Round(distKm*10) / 10,
			"duration_min": math.Round(durMin*10) / 10,
		},
	})
}

func (s *Store) quote(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RideID   string `json:"ride_id"`
		Pickup   string `json:"pickup"`
		Dropoff  string `json:"dropoff"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	v := findVehicle(in.RideID)
	if v == nil {
		writeErr(w, 404, "ride not found")
		return
	}
	if in.Pickup == "" {
		in.Pickup = "CDG"
	}
	if in.Dropoff == "" {
		in.Dropoff = "Hotel Lumière"
	}
	from, err1 := resolvePlace(in.Pickup)
	to, err2 := resolvePlace(in.Dropoff)
	if err1 != nil || err2 != nil {
		writeErr(w, 400, "could not resolve one of the locations")
		return
	}
	distM, durS, _ := osrmRoute(from, to)
	writeJSON(w, map[string]any{
		"ride_id": v.ID, "type": v.Type, "vehicle": v.Vehicle,
		"price": fare(*v, distM/1000, durS/60), "currency": "USD",
		"distance_km": math.Round(distM/10) / 100, "duration_min": math.Round(durS/6) / 10,
	})
}

func (s *Store) bookRide(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RideID     string `json:"ride_id"`
		Pickup     string `json:"pickup"`
		Dropoff    string `json:"dropoff"`
		DateTime   string `json:"datetime"`
		Passengers int    `json:"passengers"`
		TripID     string `json:"trip_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	v := findVehicle(in.RideID)
	if v == nil {
		writeErr(w, 404, "ride not found")
		return
	}
	if in.Pickup == "" {
		in.Pickup = "CDG"
	}
	if in.Dropoff == "" {
		in.Dropoff = "Hotel Lumière"
	}
	if in.DateTime == "" {
		in.DateTime = "2026-09-15T17:45"
	}
	if in.Passengers <= 0 {
		in.Passengers = 2
	}
	if in.TripID == "" {
		in.TripID = "TRIP-001"
	}
	from, err1 := resolvePlace(in.Pickup)
	to, err2 := resolvePlace(in.Dropoff)
	if err1 != nil || err2 != nil {
		writeErr(w, 400, "could not resolve one of the locations")
		return
	}
	distM, durS, _ := osrmRoute(from, to)
	distKm, durMin := distM/1000.0, durS/60.0

	s.mu.Lock()
	id := s.newID("RD")
	b := Booking{
		BookingID: id, TripID: in.TripID, Status: "CONFIRMED",
		Ride: Ride{ID: v.ID, Type: v.Type, Vehicle: v.Vehicle, Capacity: v.Capacity,
			Price: fare(*v, distKm, durMin), Currency: "USD",
			ETA: fmt.Sprintf("%d min", int(math.Round(durMin))),
			DistanceKm: math.Round(distKm*10) / 10, DurationMin: math.Round(durMin*10) / 10},
		Pickup: in.Pickup, Dropoff: in.Dropoff, DateTime: in.DateTime, Passengers: in.Passengers,
		Price: fare(*v, distKm, durMin), Currency: "USD",
		DistanceKm: math.Round(distKm*10) / 10, DurationMin: math.Round(durMin*10) / 10,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	s.bookings[id] = b
	s.mu.Unlock()
	writeJSON(w, map[string]any{"booking": b})
}

func (s *Store) modifyRide(w http.ResponseWriter, r *http.Request) {
	var in struct {
		BookingID string `json:"booking_id"`
		DateTime  string `json:"datetime"`
		Dropoff   string `json:"dropoff"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	s.mu.Lock()
	b, ok := s.bookings[in.BookingID]
	if ok {
		if in.DateTime != "" {
			b.DateTime = in.DateTime
		}
		if in.Dropoff != "" {
			b.Dropoff = in.Dropoff
		}
		b.Status = "MODIFIED"
		s.bookings[in.BookingID] = b
	}
	s.mu.Unlock()
	if !ok {
		writeErr(w, 404, "booking not found")
		return
	}
	writeJSON(w, map[string]any{"booking": b})
}

func (s *Store) cancelRide(w http.ResponseWriter, r *http.Request) {
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

func findVehicle(id string) *vehicle {
	for i := range vehicles {
		if vehicles[i].ID == id {
			return &vehicles[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// HTMX fragments (human mode)
// ---------------------------------------------------------------------------

func (s *Store) htmxSearch(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	pickup := r.FormValue("pickup")
	dropoff := r.FormValue("dropoff")
	from, err1 := resolvePlace(pickup)
	to, err2 := resolvePlace(dropoff)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err1 != nil || err2 != nil {
		_, _ = w.Write([]byte(`<p class="muted">Could not find one of those places — try selecting from the suggestions.</p>`))
		return
	}
	distM, durS, _ := osrmRoute(from, to)
	distKm, durMin := distM/1000.0, durS/60.0
	var sb strings.Builder
	sb.WriteString(`<p class="muted">Route: ` + pickup + ` → ` + dropoff + ` · ` +
		fmt.Sprintf("%.1f", distKm) + ` km · ` + fmt.Sprintf("%.0f", durMin) + ` min</p>`)
	for _, v := range vehicles {
		sb.WriteString(`<div class="card">`)
		sb.WriteString(`<div class="card-top"><strong>` + v.Type + `</strong> <span class="price">$` + strconv.Itoa(fare(v, distKm, durMin)) + `</span></div>`)
		sb.WriteString(`<div class="row muted">` + v.Vehicle + ` · up to ` + strconv.Itoa(v.Capacity) + ` pax · ETA ` + fmt.Sprintf("%.0f", durMin) + ` min</div>`)
		sb.WriteString(`<button class="btn" hx-post="/book" hx-vals='{"ride_id":"` + v.ID + `","pickup":"` + jsSafe(pickup) + `","dropoff":"` + jsSafe(dropoff) + `"}' hx-target="#bookings" hx-swap="innerHTML">Book</button>`)
		sb.WriteString(`</div>`)
	}
	_, _ = w.Write([]byte(sb.String()))
}

func (s *Store) htmxBook(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	v := findVehicle(r.FormValue("ride_id"))
	if v == nil {
		_, _ = w.Write([]byte(`<p class="muted">Ride not found.</p>`))
		return
	}
	pickup := r.FormValue("pickup")
	dropoff := r.FormValue("dropoff")
	if pickup == "" {
		pickup = "CDG"
	}
	if dropoff == "" {
		dropoff = "Hotel Lumière"
	}
	from, err1 := resolvePlace(pickup)
	to, err2 := resolvePlace(dropoff)
	if err1 != nil || err2 != nil {
		_, _ = w.Write([]byte(`<p class="muted">Could not resolve location.</p>`))
		return
	}
	distM, durS, _ := osrmRoute(from, to)
	distKm, durMin := distM/1000.0, durS/60.0
	s.mu.Lock()
	id := s.newID("RD")
	b := Booking{
		BookingID: id, TripID: "TRIP-001", Status: "CONFIRMED",
		Ride: Ride{ID: v.ID, Type: v.Type, Vehicle: v.Vehicle, Capacity: v.Capacity,
			Price: fare(*v, distKm, durMin), Currency: "USD", ETA: fmt.Sprintf("%.0f min", durMin),
			DistanceKm: math.Round(distKm*10) / 10, DurationMin: math.Round(durMin*10) / 10},
		Pickup: pickup, Dropoff: dropoff, DateTime: "2026-09-15T17:45", Passengers: 2,
		Price: fare(*v, distKm, durMin), Currency: "USD",
		DistanceKm: math.Round(distKm*10) / 10, DurationMin: math.Round(durMin*10) / 10,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
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
		sb.WriteString(`<div class="card-top"><strong>` + b.Ride.Type + `</strong> <span class="badge">` + b.Status + `</span><span class="price">$` + strconv.Itoa(b.Price) + `</span></div>`)
		sb.WriteString(`<div class="row muted">` + b.BookingID + ` · ` + b.Pickup + ` → ` + b.Dropoff + ` · ` + fmt.Sprintf("%.1f", b.DistanceKm) + ` km · ` + b.DateTime + `</div>`)
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

func jsSafe(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`)
}

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
