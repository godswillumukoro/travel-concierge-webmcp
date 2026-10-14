package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFlightSearchToBooking(t *testing.T) {
	t.Setenv("DUFFEL_API_KEY", "")
	s := newStore()

	// Invoke the same JSON handlers used by WebMCP, without opening a socket.
	call := func(handler http.HandlerFunc, path string, input, output any) {
		t.Helper()
		body, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		withJSON(handler).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d: %s", path, rec.Code, rec.Body.String())
		}
		if err := json.Unmarshal(rec.Body.Bytes(), output); err != nil {
			t.Fatalf("%s: decode response: %v", path, err)
		}
	}

	var search struct {
		Flights []Flight `json:"flights"`
	}
	// An unfiltered search avoids depending on mock routes, dates, or prices.
	call(s.searchFlights, "/api/search_flights", map[string]any{"travelers": 2}, &search)
	if len(search.Flights) == 0 {
		t.Fatal("search returned no mock flights")
	}
	// Prefer a result other than the first to catch booking a default flight.
	selected := search.Flights[len(search.Flights)-1]
	if selected.ID == "" {
		t.Fatal("selected flight has no ID")
	}
	var booked struct {
		Booking Booking `json:"booking"`
	}
	call(s.bookFlight, "/api/book_flight", map[string]any{
		"flight_id": selected.ID, "travelers": 2,
	}, &booked)
	if booked.Booking.BookingID == "" || booked.Booking.Status != "CONFIRMED" {
		t.Fatalf("expected a confirmed booking, got %+v", booked.Booking)
	}

	assertFlight := func(actual Flight) {
		t.Helper()
		// Search adds these derived display estimates; booking stores the
		// inventory flight. Compare every other field, including its ID.
		expected := selected
		expected.DistanceKm, expected.Duration = 0, ""
		actual.DistanceKm, actual.Duration = 0, ""
		if actual != expected {
			t.Fatal("booking lost the selected flight")
		}
	}
	assertFlight(booked.Booking.Flight)
	saved, ok := s.bookings[booked.Booking.BookingID]
	if !ok {
		t.Fatal("booking was not saved")
	}
	assertFlight(saved.Flight)
}
