package main

// Real airport data (codes, cities, coordinates) so flight search, autocomplete
// and distance/duration feel real. Pricing/inventory remain deterministic mock.

import (
	"math"
	"sort"
	"strings"
)

type airport struct {
	Code    string  `json:"code"`
	City    string  `json:"city"`
	Country string  `json:"country"`
	Name    string  `json:"name"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
}

var airports = map[string]airport{
	"LOS": {"LOS", "Lagos", "Nigeria", "Murtala Muhammed International", 6.5774, 3.3212},
	"ABV": {"ABV", "Abuja", "Nigeria", "Nnamdi Azikiwe International", 9.0068, 7.2632},
	"ACC": {"ACC", "Accra", "Ghana", "Kotoka International", 5.6052, -0.1668},
	"NBO": {"NBO", "Nairobi", "Kenya", "Jomo Kenyatta International", -1.3192, 36.9278},
	"JNB": {"JNB", "Johannesburg", "South Africa", "O.R. Tambo International", -26.1392, 28.2460},
	"CPT": {"CPT", "Cape Town", "South Africa", "Cape Town International", -33.9648, 18.6017},
	"CAI": {"CAI", "Cairo", "Egypt", "Cairo International", 30.1219, 31.4056},
	"CDG": {"CDG", "Paris", "France", "Charles de Gaulle", 49.0097, 2.5479},
	"ORY": {"ORY", "Paris", "France", "Orly", 48.7262, 2.3652},
	"LHR": {"LHR", "London", "United Kingdom", "Heathrow", 51.4700, -0.4543},
	"LGW": {"LGW", "London", "United Kingdom", "Gatwick", 51.1537, -0.1821},
	"AMS": {"AMS", "Amsterdam", "Netherlands", "Schiphol", 52.3105, 4.7683},
	"FRA": {"FRA", "Frankfurt", "Germany", "Frankfurt am Main", 50.0379, 8.5622},
	"BER": {"BER", "Berlin", "Germany", "Berlin Brandenburg", 52.3667, 13.5033},
	"MAD": {"MAD", "Madrid", "Spain", "Adolfo Suárez Madrid–Barajas", 40.4983, -3.5676},
	"BCN": {"BCN", "Barcelona", "Spain", "Josep Tarradellas Barcelona–El Prat", 41.2974, 2.0833},
	"FCO": {"FCO", "Rome", "Italy", "Leonardo da Vinci–Fiumicino", 41.8003, 12.2389},
	"ZRH": {"ZRH", "Zurich", "Switzerland", "Zurich Airport", 47.4647, 8.5492},
	"IST": {"IST", "Istanbul", "Türkiye", "Istanbul Airport", 41.2753, 28.7519},
	"DOH": {"DOH", "Doha", "Qatar", "Hamad International", 25.2731, 51.6081},
	"DXB": {"DXB", "Dubai", "United Arab Emirates", "Dubai International", 25.2532, 55.3657},
	"SIN": {"SIN", "Singapore", "Singapore", "Changi", 1.3644, 103.9915},
	"NRT": {"NRT", "Tokyo", "Japan", "Narita International", 35.7720, 140.3929},
	"HND": {"HND", "Tokyo", "Japan", "Haneda", 35.5494, 139.7798},
	"ICN": {"ICN", "Seoul", "South Korea", "Incheon International", 37.4602, 126.4407},
	"DEL": {"DEL", "Delhi", "India", "Indira Gandhi International", 28.5562, 77.1000},
	"JFK": {"JFK", "New York", "United States", "John F. Kennedy International", 40.6413, -73.7781},
	"EWR": {"EWR", "New York", "United States", "Newark Liberty International", 40.6895, -74.1745},
	"ATL": {"ATL", "Atlanta", "United States", "Hartsfield–Jackson Atlanta", 33.6407, -84.4277},
	"ORD": {"ORD", "Chicago", "United States", "O'Hare International", 41.9742, -87.9073},
	"LAX": {"LAX", "Los Angeles", "United States", "Los Angeles International", 33.9416, -118.4085},
	"SFO": {"SFO", "San Francisco", "United States", "San Francisco International", 37.6213, -122.3790},
	"YYZ": {"YYZ", "Toronto", "Canada", "Toronto Pearson International", 43.6777, -79.6248},
	"GRU": {"GRU", "São Paulo", "Brazil", "São Paulo/Guarulhos International", -23.4356, -46.4731},
	"MEX": {"MEX", "Mexico City", "Mexico", "Benito Juárez International", 19.4363, -99.0721},
}

// greatCircleKm returns the haversine distance between two airports in km.
func greatCircleKm(a, b airport) float64 {
	const R = 6371.0
	la1, lo1 := a.Lat*math.Pi/180, a.Lon*math.Pi/180
	la2, lo2 := b.Lat*math.Pi/180, b.Lon*math.Pi/180
	dLat, dLon := la2-la1, lo2-lo1
	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(la1)*math.Cos(la2)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * R * math.Asin(math.Sqrt(h))
}

// flightDuration estimates flight time from distance (cruise ~850 km/h + taxi).
func flightDuration(distKm float64) string {
	hours := distKm/850.0 + 0.75
	h := int(hours)
	m := int((hours - float64(h)) * 60)
	if m >= 60 {
		h++
		m -= 60
	}
	return itoa2(h) + "h " + itoa2(m) + "m"
}

// flightDistance returns rounded distance (km) and estimated duration between
// two airport codes, or (0, "") if either code is unknown.
func flightDistance(origin, dest string) (float64, string) {
	a, ok1 := airports[strings.ToUpper(origin)]
	b, ok2 := airports[strings.ToUpper(dest)]
	if !ok1 || !ok2 {
		return 0, ""
	}
	d := greatCircleKm(a, b)
	return math.Round(d*10) / 10, flightDuration(d)
}

func itoa2(n int) string {
	if n < 10 {
		return "0" + string(rune('0'+n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

// resolveAirportCode maps a free-text airport input (code, city, or name) to an
// IATA code for the search APIs.
func resolveAirportCode(s string) string {
	s = strings.TrimSpace(s)
	if _, ok := airports[strings.ToUpper(s)]; ok {
		return strings.ToUpper(s)
	}
	ls := strings.ToLower(s)
	for code, a := range airports {
		if strings.Contains(strings.ToLower(a.City), ls) || strings.Contains(strings.ToLower(a.Name), ls) {
			return code
		}
	}
	return strings.ToUpper(s)
}

// allAirports returns a sorted list of "CODE — City, Country" strings (for UI autocomplete).
func allAirports() []string {
	out := make([]string, 0, len(airports))
	for _, a := range airports {
		out = append(out, a.Code+" — "+a.City+", "+a.Country)
	}
	sort.Strings(out)
	return out
}

// matchAirports returns airports whose code, city or name contains q (case-insensitive).
func matchAirports(q string) []airport {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return nil
	}
	out := []airport{}
	for _, a := range airports {
		if strings.Contains(strings.ToLower(a.Code), q) ||
			strings.Contains(strings.ToLower(a.City), q) ||
			strings.Contains(strings.ToLower(a.Name), q) {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	if len(out) > 12 {
		out = out[:12]
	}
	return out
}
