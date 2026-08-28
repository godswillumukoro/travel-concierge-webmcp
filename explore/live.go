package main

// Photon (OpenStreetMap) live-places client — free, keyless.
// Used to enrich search results with real named places near a destination.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// LivePlace is a real place returned by Photon.
type LivePlace struct {
	Name    string
	Lat     float64
	Lon     float64
	Address string
}

// Major-city gazetteer so the golden trip and common demos work offline.
var cityCoords = map[string][2]float64{ // lat, lon
	"paris":        {48.8566, 2.3522},
	"lagos":        {6.5244, 3.3792},
	"london":       {51.5074, -0.1278},
	"new york":     {40.7128, -74.0060},
	"tokyo":        {35.6762, 139.6503},
	"dubai":        {25.2048, 55.2708},
	"berlin":       {52.5200, 13.4050},
	"amsterdam":    {52.3676, 4.9041},
	"rome":         {41.9028, 12.4964},
	"madrid":       {40.4168, -3.7038},
	"barcelona":    {41.3874, 2.1686},
	"san francisco": {37.7749, -122.4194},
	"singapore":    {1.3521, 103.8198},
	"accra":        {5.6037, -0.1870},
	"nairobi":      {-1.2921, 36.8219},
	"cairo":        {30.0444, 31.2357},
	"johannesburg": {-26.2041, 28.0473},
	"cape town":    {-33.9249, 18.4241},
}

type photonFeature struct {
	Properties struct {
		Name    string `json:"name"`
		Street  string `json:"street"`
		City    string `json:"city"`
		Country string `json:"country"`
	} `json:"properties"`
	Geometry struct {
		Coordinates []float64 `json:"coordinates"` // [lon, lat]
	} `json:"geometry"`
}

type photonList struct {
	Features []photonFeature `json:"features"`
}

var (
	liveMu    sync.Mutex
	liveCache = map[string][]LivePlace{}
	liveHTTP  = &http.Client{Timeout: 8 * time.Second}
)

const liveUA = "concierge-demo/1.0 (contact: demo@example.com)"

// GeocodeLocation resolves a free-text place name to coordinates, trying the
// city gazetteer first and then Photon.
func GeocodeLocation(name string) (float64, float64, error) {
	name = strings.TrimSpace(name)
	if c, ok := cityCoords[strings.ToLower(name)]; ok {
		return c[0], c[1], nil
	}
	pl := photonQuery(name, "", 0, 0)
	if len(pl) > 0 {
		return pl[0].Lat, pl[0].Lon, nil
	}
	return 0, 0, fmt.Errorf("could not geocode %q", name)
}

// LivePlacesNear returns real places of a given OSM tag near a coordinate.
func LivePlacesNear(q, osmTag string, lat, lon float64) []LivePlace {
	return photonQuery(q, osmTag, lat, lon)
}

func photonQuery(q, osmTag string, lat, lon float64) []LivePlace {
	key := q + "|" + osmTag + "|" + fmt.Sprintf("%.3f,%.3f", lat, lon)
	liveMu.Lock()
	if c, ok := liveCache[key]; ok {
		liveMu.Unlock()
		return c
	}
	liveMu.Unlock()

	u := "https://photon.komoot.io/api/?q=" + url.QueryEscape(q) + "&limit=12&lang=en"
	if osmTag != "" {
		u += "&osm_tag=" + url.QueryEscape(osmTag)
	}
	if lat != 0 || lon != 0 {
		const d = 0.18 // bounding-box half-width in degrees (~20 km)
		u += fmt.Sprintf("&bbox=%.4f,%.4f,%.4f,%.4f", lon-d, lat-d, lon+d, lat+d)
	}
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", liveUA)
	resp, err := liveHTTP.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil
	}
	var pr photonList
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return nil
	}
	out := make([]LivePlace, 0, len(pr.Features))
	for _, f := range pr.Features {
		if f.Properties.Name == "" || len(f.Geometry.Coordinates) < 2 {
			continue
		}
		// skip generic/unnamed POIs for category searches (e.g. name == "restaurant")
		if osmTag != "" && strings.EqualFold(f.Properties.Name, q) {
			continue
		}
		addr := strings.Trim(strings.Join([]string{f.Properties.Street, f.Properties.City, f.Properties.Country}, ", "), ", ")
		out = append(out, LivePlace{
			Name:    f.Properties.Name,
			Lat:     f.Geometry.Coordinates[1],
			Lon:     f.Geometry.Coordinates[0],
			Address: addr,
		})
	}
	liveMu.Lock()
	liveCache[key] = out
	liveMu.Unlock()
	return out
}

// handlePlaces powers the type-ahead location dropdown (free text -> places).
func handlePlaces(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	results := photonQuery(q, "", 0, 0)
	type placeOut struct {
		Name    string  `json:"name"`
		Address string  `json:"address"`
		Lat     float64 `json:"lat"`
		Lon     float64 `json:"lon"`
	}
	out := make([]placeOut, 0, len(results))
	for _, p := range results {
		out = append(out, placeOut{Name: p.Name, Address: p.Address, Lat: p.Lat, Lon: p.Lon})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"places": out})
}
