package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func searchJSON(t *testing.T, s *Store, body string, status int) []map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	withJSON(s.searchFlights)(rec, httptest.NewRequest(http.MethodPost, "/api/search_flights", strings.NewReader(body)))
	if rec.Code != status {
		t.Fatalf("status = %d, want %d: %s", rec.Code, status, rec.Body.String())
	}
	if status != http.StatusOK {
		return nil
	}
	var out struct {
		Flights []map[string]any `json:"flights"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Flights == nil {
		t.Fatal("flights should be an array, including for empty results")
	}
	return out.Flights
}

func searchIDs(results []map[string]any) []string {
	ids := make([]string, 0, len(results))
	for _, f := range results {
		ids = append(ids, f["flight_id"].(string))
	}
	return ids
}

func TestSearchMockContractAndOptions(t *testing.T) {
	t.Setenv("DUFFEL_API_KEY", "")
	original := append([]Flight(nil), flights...)
	s := newStore()
	cases := []struct {
		name, body string
		ids        []string
	}{
		{"default", `{}`, []string{"FL-001", "FL-002", "FL-003", "FL-004", "FL-005"}},
		{"explicit defaults", `{"nonstop":false,"sort_by_price":""}`, []string{"FL-001", "FL-002", "FL-003", "FL-004", "FL-005"}},
		{"route case insensitive", `{"origin":"los","destination":"cdg"}`, []string{"FL-001", "FL-002", "FL-003", "FL-004", "FL-005"}},
		{"ascending", `{"sort_by_price":"asc"}`, []string{"FL-005", "FL-001", "FL-002", "FL-003", "FL-004"}},
		{"descending", `{"sort_by_price":"desc"}`, []string{"FL-004", "FL-003", "FL-002", "FL-001", "FL-005"}},
		{"nonstop empty", `{"nonstop":true}`, []string{}},
		{"nonstop sorted empty", `{"nonstop":true,"sort_by_price":"desc"}`, []string{}},
		{"no route matches", `{"origin":"JFK"}`, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			results := searchJSON(t, s, tc.body, http.StatusOK)
			if got := searchIDs(results); !reflect.DeepEqual(got, tc.ids) {
				t.Fatalf("IDs = %v, want %v", got, tc.ids)
			}
			for _, f := range results {
				if _, ok := f["id"]; ok {
					t.Fatal("search returned legacy id")
				}
				if _, ok := f["price"].(float64); !ok {
					t.Fatal("price is not numeric")
				}
				if f["currency"] != "USD" || !strings.HasPrefix(f["price_display"].(string), "USD ") || !strings.HasSuffix(f["price_display"].(string), ".00") {
					t.Fatalf("bad display: %v", f)
				}
				if f["distance_km"].(float64) <= 0 || f["duration"] == "" {
					t.Fatal("missing mock distance mapping")
				}
			}
		})
	}
	for _, body := range []string{`{"sort_by_price":"cheap"}`, `{"nonstop":"true"}`, `{`} {
		searchJSON(t, s, body, http.StatusBadRequest)
	}
	if !reflect.DeepEqual(flights, original) {
		t.Fatal("search mutated inventory")
	}
}

func TestSearchFilterAndStableNumericSort(t *testing.T) {
	fixture := []Flight{
		{ID: "ten", Stops: 0, Price: 10}, {ID: "two-a", Stops: 0, Price: 2},
		{ID: "connection", Stops: 1, Price: 1}, {ID: "two-b", Stops: 0, Price: 2},
	}
	for _, tc := range []struct {
		order string
		ids   []string
	}{
		{"", []string{"ten", "two-a", "two-b"}},
		{"asc", []string{"two-a", "two-b", "ten"}},
		{"desc", []string{"ten", "two-a", "two-b"}},
	} {
		got := applySearchOptions(append([]Flight(nil), fixture...), searchInput{Nonstop: true, SortByPrice: tc.order})
		ids := make([]string, 0, len(got))
		for _, f := range got {
			ids = append(ids, f.ID)
		}
		if !reflect.DeepEqual(ids, tc.ids) {
			t.Fatalf("%q: %v, want %v", tc.order, ids, tc.ids)
		}
	}
}

type searchTransport func(*http.Request) (*http.Response, error)

func (f searchTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func stubSearchDuffel(t *testing.T) {
	t.Helper()
	t.Setenv("DUFFEL_API_KEY", "search-test-key")
	old := duffelHTTP
	t.Cleanup(func() { duffelHTTP = old })
	duffelHTTP = &http.Client{Transport: searchTransport(func(r *http.Request) (*http.Response, error) {
		var input struct {
			Data struct {
				Passengers []any `json:"passengers"`
			} `json:"data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		if len(input.Data.Passengers) != 3 {
			t.Fatalf("travelers not forwarded: %+v", input)
		}
		body := `{"data":{"offers":[
   {"id":"live-direct","total_amount":"342.00","total_currency":"USD","owner":{"name":"Test Air"},"slices":[{"origin":{"iata_code":"LOS"},"destination":{"iata_code":"CDG"},"segments":[{"departing_at":"out","arriving_at":"arrival","operating_carrier_flight_number":"TA1"}]}]},
   {"id":"live-return-stop","total_amount":"200.00","total_currency":"USD","slices":[{"segments":[{"departing_at":"out","arriving_at":"out-arrival"}]},{"segments":[{"departing_at":"return","arriving_at":"connection"},{"departing_at":"connection","arriving_at":"final"}]}]},
   {"id":"invalid","slices":[]}
  ]}}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
}

func TestSearchDuffelCombinedResults(t *testing.T) {
	stubSearchDuffel(t)
	s := newStore()
	results := searchJSON(t, s, `{"origin":"LOS","destination":"CDG","travelers":3}`, http.StatusOK)
	want := []string{"FL-001", "FL-002", "FL-003", "FL-004", "FL-005", "live-return-stop", "live-direct"}
	if !reflect.DeepEqual(searchIDs(results), want) {
		t.Fatalf("default combined order = %v", searchIDs(results))
	}
	results = searchJSON(t, s, `{"origin":"LOS","destination":"CDG","travelers":3,"nonstop":true,"sort_by_price":"desc"}`, http.StatusOK)
	if !reflect.DeepEqual(searchIDs(results), []string{"live-direct"}) {
		t.Fatalf("nonstop = %v", results)
	}
	f := results[0]
	if f["price_display"] != "USD 342.00" || f["source"] != "duffel" || f["price"] != float64(342) {
		t.Fatalf("bad mapped flight: %v", f)
	}
	if s.live["live-direct"].ID != "live-direct" {
		t.Fatal("live flight was not cached for subsequent tools")
	}
	results = searchJSON(t, s, `{"origin":"LOS","destination":"CDG","travelers":3,"sort_by_price":"asc"}`, http.StatusOK)
	if !reflect.DeepEqual(searchIDs(results), []string{"live-return-stop", "live-direct", "FL-005", "FL-001", "FL-002", "FL-003", "FL-004"}) {
		t.Fatalf("combined sort = %v", searchIDs(results))
	}
	if s.live["live-return-stop"].Stops != 1 || s.live["live-return-stop"].ReturnArrival != "final" {
		t.Fatal("return connection mapped incorrectly")
	}
}

func TestSearchHumanMockAndOptions(t *testing.T) {
	t.Setenv("DUFFEL_API_KEY", "")
	s := newStore()
	call := func(values url.Values) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/search", strings.NewReader(values.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		s.htmxSearch(rec, req)
		return rec
	}
	values := url.Values{"origin": {"LOS"}, "destination": {"CDG"}, "sort_by_price": {"asc"}}
	rec := call(values)
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, "USD 798.00") || !strings.Contains(body, "mock") || strings.Contains(body, "live · Duffel") {
		t.Fatalf("mock UI: %s", body)
	}
	if strings.Index(body, "Turkish Airlines") > strings.Index(body, "Air France") {
		t.Fatal("human sort not applied")
	}
	values.Set("sort_by_price", "desc")
	body = call(values).Body.String()
	if strings.Index(body, "British Airways") > strings.Index(body, "Air France") {
		t.Fatal("descending human sort not applied")
	}
	values.Set("nonstop", "true")
	if body = call(values).Body.String(); !strings.Contains(body, "No flights found") || strings.Contains(body, `class="card"`) {
		t.Fatalf("human nonstop: %s", body)
	}
	values.Set("sort_by_price", "invalid")
	if call(values).Code != 400 {
		t.Fatal("human invalid sort should return 400")
	}
}

func TestSearchHumanDuffelTravelerCount(t *testing.T) {
	stubSearchDuffel(t)
	req := httptest.NewRequest(http.MethodPost, "/search", strings.NewReader("origin=LOS&destination=CDG&travelers=3&nonstop=true&sort_by_price=asc"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	newStore().htmxSearch(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "USD 342.00") || !strings.Contains(body, "live · Duffel") || strings.Contains(body, "Air France") {
		t.Fatalf("live filtered UI: %s", body)
	}
}

func TestSearchMapping(t *testing.T) {
	for _, currency := range []string{"USD", "EUR", "NGN"} {
		if got := mapSearchResult(Flight{Currency: currency, Price: 342}).PriceDisplay; got != currency+" 342.00" {
			t.Fatalf("display = %q", got)
		}
	}
	for _, raw := range []string{`{}`, `{"slices":[{"segments":[]}]}`} {
		var offer duffelOffer
		if err := json.Unmarshal([]byte(raw), &offer); err != nil {
			t.Fatal(err)
		}
		if _, ok := mapDuffelOffer(offer); ok {
			t.Fatal("invalid offer accepted as a nonstop flight")
		}
	}
	rec := httptest.NewRecorder()
	servePage(rec, []string{"http://localhost:8080"})
	if rec.Code != 200 {
		t.Fatal("page did not render")
	}
	for _, expected := range []string{`name="nonstop"`, `name="sort_by_price"`, `nonstop: { type: 'boolean'`, `enum: ['asc', 'desc']`, `Returns flight_id`} {
		if !strings.Contains(rec.Body.String(), expected) {
			t.Fatalf("page missing %q", expected)
		}
	}
}
