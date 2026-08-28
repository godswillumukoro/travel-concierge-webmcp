package main

// Duffel flight-offers client (test mode).
//
// FlyNow's search returns deterministic mock flights by default. When
// DUFFEL_API_KEY is set, search also pulls live NDC offers from Duffel (test
// mode) and maps them to the same Flight shape. Booking of a Duffel offer is
// still mocked (no real order creation without passenger identity).
//
// Verified against the real API (2026-08): Duffel now requires
// `Duffel-Version: v2` and JSON bodies nested under a `data` key; offers come
// back synchronously on the offer_requests response under `data.offers`.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const duffelBase = "https://api.duffel.com"
const duffelVersion = "v2"

var duffelHTTP = &http.Client{Timeout: 30 * time.Second}

type duffelOffer struct {
	ID            string `json:"id"`
	TotalAmount   string `json:"total_amount"`
	TotalCurrency string `json:"total_currency"`
	Owner         struct {
		Name          string `json:"name"`
		LogoSymbolURL string `json:"logo_symbol_url"`
	} `json:"owner"`
	Slices []struct {
		Origin struct {
			IataCode string `json:"iata_code"`
		} `json:"origin"`
		Destination struct {
			IataCode string `json:"iata_code"`
		} `json:"destination"`
		Segments []struct {
			DepartingAt                  string `json:"departing_at"`
			ArrivingAt                   string `json:"arriving_at"`
			OperatingCarrierFlightNumber string `json:"operating_carrier_flight_number"`
		} `json:"segments"`
	} `json:"slices"`
}

func duffelSearch(origin, dest, depart, ret string, travelers int) ([]Flight, error) {
	if os.Getenv("DUFFEL_API_KEY") == "" {
		return nil, nil // not configured -> fall back to mock
	}
	if travelers <= 0 {
		travelers = 2
	}

	slices := []map[string]any{
		{"origin": strings.ToUpper(origin), "destination": strings.ToUpper(dest), "departure_date": depart},
	}
	if ret != "" {
		slices = append(slices, map[string]any{"origin": strings.ToUpper(dest), "destination": strings.ToUpper(origin), "departure_date": ret})
	}
	passengers := make([]map[string]any, travelers)
	for i := range passengers {
		passengers[i] = map[string]any{"type": "adult"}
	}

	var resp struct {
		Data struct {
			Offers []duffelOffer `json:"offers"`
		} `json:"data"`
	}
	if err := duffelCall("POST", "/air/offer_requests", map[string]any{
		"data": map[string]any{"slices": slices, "passengers": passengers, "cabin_class": "economy"},
	}, &resp); err != nil {
		return nil, err
	}

	out := make([]Flight, 0, len(resp.Data.Offers))
	for _, o := range resp.Data.Offers {
		if len(o.Slices) == 0 {
			continue
		}
		s0 := o.Slices[0]
		price, _ := strconv.ParseFloat(o.TotalAmount, 64)
		f := Flight{
			ID:          o.ID,
			Airline:     o.Owner.Name,
			Origin:      s0.Origin.IataCode,
			Destination: s0.Destination.IataCode,
			Stops:       max(0, len(s0.Segments)-1),
			Price:       int(price + 0.5),
			Currency:    o.TotalCurrency,
			Cabin:       "Economy",
			LogoURL:     o.Owner.LogoSymbolURL,
		}
		if len(s0.Segments) > 0 {
			f.FlightNumber = s0.Segments[0].OperatingCarrierFlightNumber
			f.Departure = s0.Segments[0].DepartingAt
			f.Arrival = s0.Segments[0].ArrivingAt
		}
		if len(o.Slices) > 1 && len(o.Slices[1].Segments) > 0 {
			f.ReturnDeparture = o.Slices[1].Segments[0].DepartingAt
			f.ReturnArrival = o.Slices[1].Segments[0].ArrivingAt
		}
		if d, dur := flightDistance(f.Origin, f.Destination); d > 0 {
			f.DistanceKm = d
			f.Duration = dur
		}
		out = append(out, f)
	}

	// Cheapest first; cap so the response stays readable for the agent/UI.
	sort.Slice(out, func(i, j int) bool { return out[i].Price < out[j].Price })
	if len(out) > 12 {
		out = out[:12]
	}
	return out, nil
}

func duffelCall(method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, duffelBase+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+os.Getenv("DUFFEL_API_KEY"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Duffel-Version", duffelVersion)
	resp, err := duffelHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("duffel %s: %s", path, strings.TrimSpace(string(b)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
