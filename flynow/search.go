package main

import (
	"fmt"
	"sort"
	"strings"
)

type searchInput struct {
	Origin        string `json:"origin"`
	Destination   string `json:"destination"`
	DepartureDate string `json:"departure_date"`
	ReturnDate    string `json:"return_date"`
	Travelers     int    `json:"travelers"`
	Nonstop       bool   `json:"nonstop"`
	SortByPrice   string `json:"sort_by_price"`
}

func (in searchInput) validSort() bool {
	return in.SortByPrice == "" || in.SortByPrice == "asc" || in.SortByPrice == "desc"
}

// searchFlight changes only the search response identifier; other APIs keep Flight.
type searchFlight struct {
	ID              string  `json:"flight_id"`
	Airline         string  `json:"airline"`
	FlightNumber    string  `json:"flight_number"`
	Origin          string  `json:"origin"`
	Destination     string  `json:"destination"`
	Departure       string  `json:"departure"`
	Arrival         string  `json:"arrival"`
	ReturnDeparture string  `json:"return_departure"`
	ReturnArrival   string  `json:"return_arrival"`
	Stops           int     `json:"stops"`
	Price           int     `json:"price"`
	Currency        string  `json:"currency"`
	Refundable      bool    `json:"refundable"`
	Cabin           string  `json:"cabin"`
	DistanceKm      float64 `json:"distance_km"`
	Duration        string  `json:"duration"`
	Source          string  `json:"source,omitempty"`
	LogoURL         string  `json:"logo_url,omitempty"`
}

type searchResult struct {
	searchFlight
	PriceDisplay string `json:"price_display"`
}

func mapSearchResult(f Flight) searchResult {
	return searchResult{searchFlight: searchFlight(f), PriceDisplay: formatPrice(f.Currency, f.Price)}
}

func mapMockFlight(f Flight) Flight {
	if d, dur := flightDistance(f.Origin, f.Destination); d > 0 {
		f.DistanceKm, f.Duration = d, dur
	}
	return f
}

// search serves both HTMX and WebMCP. Omitted options retain inventory ordering.
func (s *Store) search(in searchInput) []Flight {
	if in.Travelers <= 0 {
		in.Travelers = 2
	}
	results := make([]Flight, 0)
	for _, f := range flights {
		if in.Origin != "" && !strings.EqualFold(f.Origin, in.Origin) {
			continue
		}
		if in.Destination != "" && !strings.EqualFold(f.Destination, in.Destination) {
			continue
		}
		results = append(results, mapMockFlight(f))
	}
	if live, err := duffelSearch(in.Origin, in.Destination, in.DepartureDate, in.ReturnDate, in.Travelers); err == nil {
		s.mu.Lock()
		for _, f := range live {
			s.live[f.ID] = f
			results = append(results, f)
		}
		s.mu.Unlock()
	}
	return applySearchOptions(results, in)
}

func applySearchOptions(results []Flight, in searchInput) []Flight {
	if in.Nonstop {
		filtered := make([]Flight, 0, len(results))
		for _, f := range results {
			if f.Stops == 0 {
				filtered = append(filtered, f)
			}
		}
		results = filtered
	}
	if in.SortByPrice != "" {
		sort.SliceStable(results, func(i, j int) bool {
			if in.SortByPrice == "desc" {
				return results[i].Price > results[j].Price
			}
			return results[i].Price < results[j].Price
		})
	}
	return results
}

func formatPrice(currency string, price int) string {
	return fmt.Sprintf("%s %.2f", currency, float64(price))
}
