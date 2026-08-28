package main

// Hotelbeds APItude hotel-availability client (test sandbox).
//
// Stayly returns deterministic mock stays + Photon real hotels by default. When
// HOTELBEDS_API_KEY / HOTELBEDS_SECRET are set, search also pulls live hotel
// offers from Hotelbeds GDS and maps them to the same Stay shape. Reservation is
// still mocked.
//
// Auth: SHA256(apiKey + secret + unixSeconds) sent as X-Signature, with Api-Key.
// Verified against the real test API (2026-08).

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const hbBase = "https://api.test.hotelbeds.com"

var (
	hbMu    sync.Mutex
	hbCache = map[string][]Stay{}
	hbHTTP  = &http.Client{Timeout: 25 * time.Second}
)

func hotelbedsConfigured() bool {
	return os.Getenv("HOTELBEDS_API_KEY") != "" && os.Getenv("HOTELBEDS_SECRET") != ""
}

// hotelbedsSearch returns live hotel offers near a location, mapped to Stay.
func hotelbedsSearch(location, checkIn, checkOut string, guests int) ([]Stay, error) {
	if !hotelbedsConfigured() {
		return nil, nil
	}
	if guests <= 0 {
		guests = 2
	}
	lat, lon, err := GeocodeLocation(location)
	if err != nil {
		return nil, err
	}
	cacheKey := fmt.Sprintf("%s|%s|%s|%.2f,%.2f", checkIn, checkOut, location, lat, lon)
	hbMu.Lock()
	if c, ok := hbCache[cacheKey]; ok {
		hbMu.Unlock()
		return c, nil
	}
	hbMu.Unlock()

	body := map[string]any{
		"stay":        map[string]any{"checkIn": checkIn, "checkOut": checkOut},
		"occupancies": []map[string]any{{"rooms": 1, "adults": guests, "children": 0}},
		"geolocation": map[string]any{"latitude": lat, "longitude": lon, "radius": 12, "unit": "km"},
		"filter":      map[string]any{"maxHotels": 8},
	}
	var resp struct {
		Hotels struct {
			Hotels []struct {
				Code            int    `json:"code"`
				Name            string `json:"name"`
				CategoryCode    string `json:"categoryCode"`
				CategoryName    string `json:"categoryName"`
				DestinationName string `json:"destinationName"`
				ZoneName        string `json:"zoneName"`
				Currency        string `json:"currency"`
				MinRate         string `json:"minRate"`
			} `json:"hotels"`
		} `json:"hotels"`
	}
	if err := hbCall("POST", "/hotel-api/1.0/hotels", body, &resp); err != nil {
		return nil, err
	}

	nights := nightsBetween(checkIn, checkOut)
	out := []Stay{}
	for _, h := range resp.Hotels.Hotels {
		if h.Name == "" || h.MinRate == "" {
			continue
		}
		rate, _ := strconv.ParseFloat(h.MinRate, 64)
		ppn := int(rate/float64(nights) + 0.5)
		// Normalise to USD (approx) so the trip total stays in one currency.
		if h.Currency == "EUR" {
			ppn = int(float64(ppn) * 1.09)
		} else if h.Currency == "GBP" {
			ppn = int(float64(ppn) * 1.27)
		}
		neighborhood := h.ZoneName
		if neighborhood == "" {
			neighborhood = h.DestinationName
		}
		out = append(out, Stay{
			ID: "HB-" + strconv.Itoa(h.Code), Name: h.Name, Neighborhood: neighborhood,
			Type: h.CategoryName, Guests: guests,
			PricePerNight: ppn, Currency: "USD",
			Rating: starsToRating(h.CategoryCode), Amenities: []string{"wifi"},
		})
	}

	hbMu.Lock()
	hbCache[cacheKey] = out
	hbMu.Unlock()
	return out, nil
}

func starsToRating(cat string) float64 {
	switch strings.ToUpper(cat) {
	case "5EST", "5STARS", "5STAR":
		return 4.8
	case "4EST", "4STARS", "4STAR":
		return 4.2
	case "3EST", "3STARS", "3STAR":
		return 3.8
	case "2EST", "2STARS", "2STAR":
		return 3.4
	default:
		return 4.0
	}
}

func hbCall(method, path string, body any, out any) error {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sum := sha256.Sum256([]byte(os.Getenv("HOTELBEDS_API_KEY") + os.Getenv("HOTELBEDS_SECRET") + ts))

	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, hbBase+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Api-Key", os.Getenv("HOTELBEDS_API_KEY"))
	req.Header.Set("X-Signature", hex.EncodeToString(sum[:]))
	req.Header.Set("Content-Type", "application/json")
	resp, err := hbHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("hotelbeds %s: %s", path, strings.TrimSpace(string(b)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
