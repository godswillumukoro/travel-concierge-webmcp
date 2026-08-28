// Concierge — an agent-native travel orchestrator powered by WebMCP.
//
// Concierge is a top-level web application that embeds independent travel
// providers as cross-origin iframes, discovers their capabilities through the
// WebMCP `document.modelContext` API, and runs an agent loop that composes
// those capabilities to plan and book a whole trip.
//
// The AI loop is split between the browser and this backend:
//
//   - Browser: WebMCP tool discovery, tool execution, approvals, trip state.
//   - Backend:  model calls / deterministic planning, secrets, activity log.
//
// By default (no API key) Concierge runs a fully deterministic planner so the
// whole demo works offline. Set OPENAI_API_KEY to drive it with a real model.
package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed static
var staticFS embed.FS

// ---------------------------------------------------------------------------
// Providers
// ---------------------------------------------------------------------------

type provider struct {
	Name     string `json:"name"`
	Origin   string `json:"origin"`
	Category string `json:"category"`
	Icon     string `json:"icon"`
	Backend  string `json:"backend"`
}

func defaultProviders() []provider {
	return []provider{
		{Name: "FlyNow", Origin: "http://localhost:8081", Category: "flights", Icon: "✈", Backend: "Duffel + real airports"},
		{Name: "Stayly", Origin: "http://localhost:8082", Category: "stays", Icon: "🏠", Backend: "Hotelbeds + Photon"},
		{Name: "Dine", Origin: "http://localhost:8083", Category: "dining", Icon: "🍽", Backend: "Photon (OSM)"},
		{Name: "Explore", Origin: "http://localhost:8084", Category: "experiences", Icon: "🎟", Backend: "Photon (OSM)"},
		{Name: "Ride", Origin: "http://localhost:8085", Category: "transport", Icon: "🚕", Backend: "OSRM + Photon"},
	}
}

// ---------------------------------------------------------------------------
// Server
// ---------------------------------------------------------------------------

type activity struct {
	TS       string `json:"ts"`
	Provider string `json:"provider"`
	Tool     string `json:"tool"`
	Status   string `json:"status"`
	Summary  string `json:"summary"`
}

type server struct {
	providers []provider
	apiKey    string
	baseURL   string
	model     string
	planner   string // "deterministic" | "llm" | "" (auto)

	mu         sync.Mutex
	activities []activity
}

func main() {
	s := &server{
		providers:  loadProviders(),
		apiKey:     os.Getenv("OPENAI_API_KEY"),
		baseURL:    strings.TrimRight(getenv("OPENAI_BASE_URL", "https://api.openai.com/v1"), "/"),
		model:      getenv("OPENAI_MODEL", "gpt-4o-mini"),
		planner:    os.Getenv("PLANNER"),
		activities: []activity{},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleIndex)
	mux.HandleFunc("GET /static/", serveStatic)
	mux.HandleFunc("POST /api/agent", s.handleAgent)
	mux.HandleFunc("GET /activity", s.handleActivityList)
	mux.HandleFunc("POST /api/activity", s.handleActivityAdd)

	port := getenv("PORT", "8080")
	log.Printf("Concierge listening on http://localhost:%s (providers: %d, model: %s, planner: %s)", port, len(s.providers), s.model, s.plannerMode())
	log.Fatal(http.ListenAndServe(":"+port, withHeaders(mux)))
}

func loadProviders() []provider {
	if raw := os.Getenv("PROVIDERS"); raw != "" {
		var ps []provider
		if err := json.Unmarshal([]byte(raw), &ps); err == nil && len(ps) > 0 {
			return ps
		}
		log.Printf("invalid PROVIDERS env, using defaults")
	}
	return defaultProviders()
}

func withHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// WebMCP requires documents to live in an origin-keyed agent cluster.
		w.Header().Set("Origin-Agent-Cluster", "?1")
		w.Header().Set("Cache-Control", "no-store")
		if tok := os.Getenv("ORIGIN_TRIAL_TOKEN"); tok != "" {
			w.Header().Set("Origin-Trial", tok)
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	tpl, err := template.ParseFS(staticFS, "static/index.html")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	providersJSON, _ := json.Marshal(s.providers)
	cfg := map[string]any{
		"budget":       3500,
		"threshold":    100,
		"llm":          s.apiKey != "",
		"model":        s.model,
	}
	cfgJSON, _ := json.Marshal(cfg)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = tpl.Execute(w, map[string]any{
		"ProvidersJSON": template.JS(providersJSON),
		"ConfigJSON":    template.JS(cfgJSON),
	})
}

func serveStatic(w http.ResponseWriter, r *http.Request) {
	http.FileServer(http.FS(staticFS)).ServeHTTP(w, r)
}

// ---------------------------------------------------------------------------
// Activity log (rendered with HTMX)
// ---------------------------------------------------------------------------

func (s *server) handleActivityAdd(w http.ResponseWriter, r *http.Request) {
	var a activity
	_ = json.NewDecoder(r.Body).Decode(&a)
	a.TS = time.Now().Format("15:04:05")
	s.mu.Lock()
	s.activities = append([]activity{a}, s.activities...)
	if len(s.activities) > 100 {
		s.activities = s.activities[:100]
	}
	s.mu.Unlock()
	w.WriteHeader(204)
}

func (s *server) handleActivityList(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	acts := append([]activity(nil), s.activities...)
	s.mu.Unlock()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if len(acts) == 0 {
		_, _ = w.Write([]byte(`<p class="muted">No activity yet. Send a request to watch Concierge work.</p>`))
		return
	}
	var b strings.Builder
	for _, a := range acts {
		cls := "ok"
		if a.Status == "error" {
			cls = "error"
		} else if a.Status == "approval" {
			cls = "approval"
		}
		b.WriteString(`<div class="act ` + cls + `"><span class="act-ts">` + a.TS + `</span><span class="act-name">` + a.Provider + `.` + a.Tool + `()</span><span class="act-sum">` + a.Summary + `</span></div>`)
	}
	_, _ = w.Write([]byte(b.String()))
}

// ---------------------------------------------------------------------------
// Agent endpoint
// ---------------------------------------------------------------------------

type agentRequest struct {
	Tools    []toolDef `json:"tools"`
	Messages []message `json:"messages"`
	Config   struct {
		Budget    int `json:"budget"`
		Threshold int `json:"threshold"`
	} `json:"config"`
}

type toolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Origin      string          `json:"origin"`
	Annotations struct {
		ReadOnlyHint bool `json:"readOnlyHint"`
	} `json:"annotations"`
}

type message struct {
	Role       string     `json:"role"`
	Content    any        `json:"content"`
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type toolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type agentResponse struct {
	Type      string         `json:"type"` // "tool_call" | "final" | "error"
	Name      string         `json:"name,omitempty"`
	Arguments map[string]any `json:"arguments,omitempty"`
	Text      string         `json:"text,omitempty"`
}

func (s *server) handleAgent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	var req agentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, agentResponse{Type: "error", Text: "invalid request"})
		return
	}
	if len(req.Tools) == 0 {
		writeJSON(w, agentResponse{Type: "final", Text: "I couldn't discover any WebMCP tools from the connected services. Make sure the providers are running and your browser supports WebMCP."})
		return
	}
	var resp agentResponse
	if s.useLLM() {
		resp = s.llmStep(req)
	} else {
		resp = plannerStep(req)
	}
	writeJSON(w, resp)
}

// useLLM reports whether the model-driven path is active. "auto" (default)
// uses the LLM when a key is present, otherwise the deterministic planner.
func (s *server) useLLM() bool {
	if s.planner == "llm" {
		return true
	}
	if s.planner == "deterministic" {
		return false
	}
	return s.apiKey != ""
}

func (s *server) plannerMode() string {
	if s.useLLM() {
		return "llm"
	}
	return "deterministic"
}

// ---------------------------------------------------------------------------
// Deterministic planner (offline demo mode)
// ---------------------------------------------------------------------------

type category struct {
	key, searchTool, bookTool, searchKey, idField, bookWrapKey string
	searchArgsFn func(t trip) map[string]any
	bookArgsFn   func(item map[string]any, t trip) map[string]any
	titleWrapFn  func(wrap map[string]any) string
	scoreFn      func(item map[string]any, p prefs, remaining int) int
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func num(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case json.Number:
		n, _ := x.Int64()
		return int(n)
	}
	return 0
}

func fnum(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case json.Number:
		f, _ := x.Float64()
		return f
	}
	return 0
}

// trip is the parsed destination/dates/travelers for the deterministic planner.
type trip struct {
	origin      string // IATA code
	destination string // IATA code
	city        string // city name (stays/dining/experiences)
	depart      string // YYYY-MM-DD
	ret         string // YYYY-MM-DD
	travelers   int
}

// destinations maps free-text place names to (city, IATA airport code).
var destinations = map[string][2]string{
	"paris":        {"Paris", "CDG"},
	"lagos":        {"Lagos", "LOS"},
	"accra":        {"Accra", "ACC"},
	"ghana":        {"Accra", "ACC"},
	"abuja":        {"Abuja", "ABV"},
	"london":       {"London", "LHR"},
	"new york":     {"New York", "JFK"},
	"tokyo":        {"Tokyo", "NRT"},
	"dubai":        {"Dubai", "DXB"},
	"amsterdam":    {"Amsterdam", "AMS"},
	"berlin":       {"Berlin", "BER"},
	"rome":         {"Rome", "FCO"},
	"barcelona":    {"Barcelona", "BCN"},
	"madrid":       {"Madrid", "MAD"},
	"singapore":    {"Singapore", "SIN"},
	"nairobi":      {"Nairobi", "NBO"},
	"cairo":        {"Cairo", "CAI"},
	"cape town":    {"Cape Town", "CPT"},
	"johannesburg": {"Johannesburg", "JNB"},
}

var (
	reTravelers = regexp.MustCompile(`(?i)(\d+|one|two|three|four|five|six|couple)\s*(people|travelers|guests|passengers|adults|pax)`)
	reDuration  = regexp.MustCompile(`(?i)(\d+|one|two|three|four|five|six|seven|ten|fourteen)\s*-?\s*day`)
	wordNums    = map[string]int{"one": 1, "two": 2, "three": 3, "four": 4, "five": 5, "six": 6, "seven": 7, "ten": 10, "fourteen": 14, "couple": 2}
)

func parseNum(s string) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	if n, ok := wordNums[strings.ToLower(s)]; ok {
		return n
	}
	return 0
}

func dateOffset(days int) string { return time.Now().AddDate(0, 0, days).Format("2006-01-02") }

func addDays(date string, n int) string {
	if t, err := time.Parse("2006-01-02", date); err == nil {
		return t.AddDate(0, 0, n).Format("2006-01-02")
	}
	return date
}

// parseTrip extracts destination, origin, duration and travelers from the
// request, with sensible defaults (Lagos → Paris, 5 days, 2 travelers).
func parseTrip(text string) trip {
	low := strings.ToLower(text)
	t := trip{origin: "LOS", travelers: 2, depart: dateOffset(7)}

	// origin: "from <place>"
	if i := strings.Index(low, "from "); i >= 0 {
		rest := low[i+5:]
		for k, v := range destinations {
			if strings.HasPrefix(rest, k) {
				t.origin = v[1]
				break
			}
		}
	}

	// destination: "to <place>"
	if i := strings.Index(low, "to "); i >= 0 {
		rest := low[i+3:]
		for k, v := range destinations {
			if strings.HasPrefix(rest, k) {
				t.city, t.destination = v[0], v[1]
				break
			}
		}
	}

	// fallback: longest place mention that isn't the origin
	if t.destination == "" {
		best := ""
		for k, v := range destinations {
			if v[1] == t.origin {
				continue
			}
			if strings.Contains(low, k) && len(k) > len(best) {
				best = k
			}
		}
		if best != "" {
			t.city, t.destination = destinations[best][0], destinations[best][1]
		} else {
			t.city, t.destination = "Paris", "CDG"
		}
	}

	if m := reTravelers.FindStringSubmatch(low); m != nil {
		if n := parseNum(m[1]); n > 0 && n <= 9 {
			t.travelers = n
		}
	}

	duration := 5
	if m := reDuration.FindStringSubmatch(low); m != nil {
		if n := parseNum(m[1]); n > 0 && n <= 30 {
			duration = n
		}
	}
	t.ret = addDays(t.depart, duration)
	return t
}

var cats = []*category{
	{
		key: "flight", searchTool: "search_flights", bookTool: "book_flight",
		searchKey: "flights", idField: "flight_id", bookWrapKey: "booking",
		searchArgsFn: func(t trip) map[string]any {
			return map[string]any{"origin": t.origin, "destination": t.destination, "departure_date": t.depart, "return_date": t.ret, "travelers": t.travelers}
		},
		bookArgsFn: func(it map[string]any, t trip) map[string]any {
			return map[string]any{"flight_id": it["id"], "travelers": t.travelers, "trip_id": "TRIP-001"}
		},
		titleWrapFn: func(w map[string]any) string {
			if f, ok := w["flight"].(map[string]any); ok {
				return str(f["airline"]) + " " + str(f["flight_number"])
			}
			return "Flight"
		},
		scoreFn: scoreFlight,
	},
	{
		key: "stay", searchTool: "search_stays", bookTool: "reserve_room",
		searchKey: "stays", idField: "stay_id", bookWrapKey: "reservation",
		searchArgsFn: func(t trip) map[string]any {
			return map[string]any{"location": t.city, "check_in": t.depart, "check_out": t.ret, "guests": t.travelers}
		},
		bookArgsFn: func(it map[string]any, t trip) map[string]any {
			return map[string]any{"stay_id": it["id"], "check_in": t.depart, "check_out": t.ret, "guests": t.travelers, "trip_id": "TRIP-001"}
		},
		titleWrapFn: func(w map[string]any) string {
			if st, ok := w["stay"].(map[string]any); ok {
				return str(st["name"])
			}
			return "Stay"
		},
		scoreFn: scoreStay,
	},
	{
		key: "dining", searchTool: "search_restaurants", bookTool: "book_table",
		searchKey: "restaurants", idField: "restaurant_id", bookWrapKey: "booking",
		searchArgsFn: func(t trip) map[string]any {
			return map[string]any{"location": t.city, "date": addDays(t.depart, 1), "party_size": t.travelers}
		},
		bookArgsFn: func(it map[string]any, t trip) map[string]any {
			return map[string]any{"restaurant_id": it["id"], "date": addDays(t.depart, 1), "time": "19:30", "party_size": t.travelers, "trip_id": "TRIP-001"}
		},
		titleWrapFn: func(w map[string]any) string {
			if r, ok := w["restaurant"].(map[string]any); ok {
				return str(r["name"])
			}
			return "Dinner"
		},
		scoreFn: scoreDine,
	},
	{
		key: "experience", searchTool: "search_experiences", bookTool: "book_experience",
		searchKey: "experiences", idField: "experience_id", bookWrapKey: "booking",
		searchArgsFn: func(t trip) map[string]any {
			return map[string]any{"location": t.city, "date": addDays(t.depart, 2), "travelers": t.travelers}
		},
		bookArgsFn: func(it map[string]any, t trip) map[string]any {
			return map[string]any{"experience_id": it["id"], "date": addDays(t.depart, 2), "travelers": t.travelers, "trip_id": "TRIP-001"}
		},
		titleWrapFn: func(w map[string]any) string {
			if e, ok := w["experience"].(map[string]any); ok {
				return str(e["name"])
			}
			return "Experience"
		},
		scoreFn: scoreExperience,
	},
	{
		key: "ride", searchTool: "search_rides", bookTool: "book_ride",
		searchKey: "rides", idField: "ride_id", bookWrapKey: "booking",
		searchArgsFn: func(t trip) map[string]any {
			return map[string]any{"pickup": t.destination, "dropoff": t.city, "datetime": t.depart + "T17:45", "passengers": t.travelers}
		},
		bookArgsFn: func(it map[string]any, t trip) map[string]any {
			return map[string]any{"ride_id": it["id"], "pickup": t.destination, "dropoff": t.city, "datetime": t.depart + "T17:45", "passengers": t.travelers, "trip_id": "TRIP-001"}
		},
		titleWrapFn: func(w map[string]any) string {
			if r, ok := w["ride"].(map[string]any); ok {
				return str(r["type"])
			}
			return "Ride"
		},
		scoreFn: scoreRide,
	},
}

func (c *category) byTool(name string) bool { return name == c.searchTool || name == c.bookTool }

func plannerStep(req agentRequest) agentResponse {
	// Index assistant tool calls so tool results can be linked to names/args.
	callName := map[string]string{}
	callArgs := map[string]map[string]any{}
	for _, m := range req.Messages {
		if m.Role == "assistant" {
			for _, tc := range m.ToolCalls {
				callName[tc.ID] = tc.Function.Name
				var a map[string]any
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &a)
				callArgs[tc.ID] = a
			}
		}
	}

	// Reconstruct state by replaying messages IN ORDER (order matters for
	// disruption recovery).
	search := map[string]map[string]any{}
	rejected := map[string]map[string]bool{}
	booked := map[string]bool{}
	bookedRes := map[string]map[string]any{}
	disrupted := false
	rebooked := false
	rodeAdjusted := false
	dinnerShifted := false
	originalFlightID := ""

	for _, m := range req.Messages {
		if m.Role == "user" {
			t := strings.ToLower(contentString(m.Content))
			if strings.Contains(t, "cancel") || strings.Contains(t, "disruption") || strings.Contains(t, "rebook") {
				disrupted = true
			}
			continue
		}
		if m.Role != "tool" {
			continue
		}
		name := callName[m.ToolCallID]
		if name == "" {
			continue
		}
		raw := contentString(m.Content)
		var parsed map[string]any
		_ = json.Unmarshal([]byte(raw), &parsed)

		// Recovery-specific tools that aren't part of a normal category.
		if name == "modify_ride" {
			if parsed != nil && parsed["error"] == nil {
				if wrap, ok := parsed["booking"].(map[string]any); ok {
					bookedRes["ride"] = wrap
				}
				if disrupted {
					rodeAdjusted = true
				}
			}
			continue
		}
		if name == "cancel_flight" {
			continue // the client already cancelled it; planner keys off the user message
		}

		c := catByTool(name)
		if c == nil {
			continue
		}
		if name == c.searchTool {
			search[name] = parsed
			continue
		}
		if parsed != nil && parsed["error"] != nil {
			if strings.Contains(raw, "rejected by user") {
				if rejected[c.key] == nil {
					rejected[c.key] = map[string]bool{}
				}
				if idv, ok := callArgs[m.ToolCallID][c.idField].(string); ok {
					rejected[c.key][idv] = true
				}
			}
			continue
		}
		booked[c.key] = true
		if wrap, ok := parsed[c.bookWrapKey].(map[string]any); ok {
			bookedRes[c.key] = wrap
			if name == "book_flight" && !disrupted {
				if fl, ok := wrap["flight"].(map[string]any); ok {
					originalFlightID = str(fl["id"])
				}
			}
		}
		if disrupted {
			switch name {
			case "book_flight":
				rebooked = true
			case "book_table":
				dinnerShifted = true
			}
		}
	}

	// Phase 1: search every provider.
	t := parseTrip(userText(req))
	for _, c := range cats {
		if _, ok := search[c.searchTool]; !ok {
			return agentResponse{Type: "tool_call", Name: c.searchTool, Arguments: c.searchArgsFn(t)}
		}
	}

	// Phase 2: book each category, choosing the best match for the user's
	// preferences while respecting the remaining budget.
	p := parsePrefs(userText(req))
	budget := req.Config.Budget
	if budget <= 0 {
		budget = 3500
	}
	remaining := budget - bookedTotal(bookedRes)
	if remaining < 0 {
		remaining = 0
	}
	for _, c := range cats {
		if booked[c.key] {
			continue
		}
		items := searchItems(search[c.searchTool], c.searchKey)
		item := pickBest(items, rejected[c.key], func(it map[string]any) int {
			return c.scoreFn(it, p, remaining)
		})
		if item == nil {
			continue
		}
		return agentResponse{Type: "tool_call", Name: c.bookTool, Arguments: c.bookArgsFn(item, t)}
	}

	// Phase 3: disruption recovery.
	if disrupted {
		return recoveryStep(search, rejected, bookedRes, originalFlightID, rebooked, rodeAdjusted, dinnerShifted, p, remaining, t)
	}

	return agentResponse{Type: "final", Text: finalText(bookedRes)}
}

func catByTool(name string) *category {
	for _, c := range cats {
		if c.byTool(name) {
			return c
		}
	}
	return nil
}

// recoveryStep drives the disruption flow: rebook a replacement flight, shift
// the airport transfer, and move dinner later.
func recoveryStep(search map[string]map[string]any, rejected map[string]map[string]bool, bookedRes map[string]map[string]any, originalFlightID string, rebooked, rodeAdjusted, dinnerShifted bool, p prefs, remaining int, t trip) agentResponse {
	if !rebooked {
		// Treat the cancelled flight as unavailable and book the next best.
		if originalFlightID != "" {
			if rejected["flight"] == nil {
				rejected["flight"] = map[string]bool{}
			}
			rejected["flight"][originalFlightID] = true
		}
		items := searchItems(search["search_flights"], "flights")
		item := pickBest(items, rejected["flight"], func(it map[string]any) int { return scoreFlight(it, p, remaining) })
		if item == nil {
			return agentResponse{Type: "final", Text: "I couldn't find a replacement flight. Please try again later."}
		}
		return agentResponse{Type: "tool_call", Name: "book_flight", Arguments: map[string]any{"flight_id": item["id"], "travelers": t.travelers, "trip_id": "TRIP-001"}}
	}
	if !rodeAdjusted {
		rid := ""
		if w, ok := bookedRes["ride"]; ok {
			rid = str(w["booking_id"])
		}
		dt := ""
		if w, ok := bookedRes["flight"]; ok {
			if f, ok := w["flight"].(map[string]any); ok {
				dt = shiftTime(str(f["arrival"]), 45)
			}
		}
		return agentResponse{Type: "tool_call", Name: "modify_ride", Arguments: map[string]any{"booking_id": rid, "datetime": dt}}
	}
	if !dinnerShifted {
		rest := ""
		if w, ok := bookedRes["dining"]; ok {
			if r, ok := w["restaurant"].(map[string]any); ok {
				rest = str(r["id"])
			}
		}
		return agentResponse{Type: "tool_call", Name: "book_table", Arguments: map[string]any{"restaurant_id": rest, "date": addDays(t.depart, 1), "time": "21:30", "party_size": t.travelers, "trip_id": "TRIP-001"}}
	}
	return agentResponse{Type: "final", Text: recoveryFinal(bookedRes)}
}

func shiftTime(iso string, mins int) string {
	if iso == "" {
		return ""
	}
	for _, layout := range []string{"2006-01-02T15:04", time.RFC3339} {
		if t, err := time.Parse(layout, iso); err == nil {
			return t.Add(time.Duration(mins) * time.Minute).Format("2006-01-02T15:04")
		}
	}
	return iso
}

func recoveryFinal(res map[string]map[string]any) string {
	var b strings.Builder
	b.WriteString("Your flight was cancelled. I rebooked you on ")
	if w, ok := res["flight"]; ok {
		if f, ok := w["flight"].(map[string]any); ok {
			b.WriteString(str(f["airline"]) + " " + str(f["flight_number"]) + " arriving " + str(f["arrival"]))
		} else {
			b.WriteString("a replacement flight")
		}
	}
	b.WriteString(". I updated your airport transfer")
	if w, ok := res["ride"]; ok {
		b.WriteString(" to " + str(w["datetime"]))
	}
	b.WriteString(" and moved dinner to 9:30 PM")
	if w, ok := res["dining"]; ok {
		if r, ok := w["restaurant"].(map[string]any); ok {
			b.WriteString(" at " + str(r["name"]))
		}
	}
	b.WriteString(".\n\nUpdated trip:\n")
	total := 0
	for _, c := range cats {
		if w, ok := res[c.key]; ok {
			b.WriteString("• " + c.titleWrapFn(w) + " — $" + strconv.Itoa(num(w["price"])) + "\n")
			total += num(w["price"])
		}
	}
	b.WriteString("\nTotal: $" + strconv.Itoa(total))
	return b.String()
}

func contentString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func searchItems(res map[string]any, key string) []map[string]any {
	if res == nil {
		return nil
	}
	raw, ok := res[key].([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(raw))
	for _, r := range raw {
		if m, ok := r.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func pickBest(items []map[string]any, rejected map[string]bool, score func(map[string]any) int) map[string]any {
	best := -1 << 30
	var bestItem map[string]any
	for _, it := range items {
		if rejected[str(it["id"])] {
			continue
		}
		if s := score(it); s > best {
			best = s
			bestItem = it
		}
	}
	return bestItem
}

func userText(req agentRequest) string {
	for _, m := range req.Messages {
		if m.Role == "user" {
			return contentString(m.Content)
		}
	}
	return ""
}

func bookedTotal(res map[string]map[string]any) int {
	total := 0
	for _, w := range res {
		total += num(w["price"])
	}
	return total
}

type prefs struct {
	stay       []string
	dine       []string
	experience []string
	greatFood  bool
	memorable  bool
	flight     string
	// ride: "" (default private) | "cheap" | "luxury" | "private"
	typ        string
}

// parsePrefs extracts preference signals from the user's request.
func parsePrefs(text string) prefs {
	t := strings.ToLower(text)
	var p prefs
	if strings.Contains(t, "boutique") {
		p.stay = append(p.stay, "boutique")
	}
	if strings.Contains(t, "luxury") || strings.Contains(t, "5-star") || strings.Contains(t, "five star") {
		p.stay = append(p.stay, "luxury")
		p.typ = "luxury"
	}
	if strings.Contains(t, "apartment") {
		p.stay = append(p.stay, "apartment")
	}
	if strings.Contains(t, "cheap") || strings.Contains(t, "budget") {
		p.stay = append(p.stay, "cheap")
		p.typ = "cheap"
	}
	if strings.Contains(t, "private") {
		p.typ = "private"
	}
	if strings.Contains(t, "great food") || strings.Contains(t, "good food") || strings.Contains(t, "foodie") ||
		strings.Contains(t, "best food") || strings.Contains(t, "amazing food") {
		p.greatFood = true
	}
	for _, c := range []string{"french", "italian", "japanese", "chinese", "mexican", "indian", "thai", "seafood", "steak", "african", "nigerian"} {
		if strings.Contains(t, c) {
			p.dine = append(p.dine, c)
		}
	}
	if strings.Contains(t, "memorable") || strings.Contains(t, "iconic") || strings.Contains(t, "famous") || strings.Contains(t, "unforgettable") {
		p.memorable = true
	}
	for _, k := range []string{"museum", "cruise", "adventure", "food tour", "relax"} {
		if strings.Contains(t, k) {
			p.experience = append(p.experience, k)
		}
	}
	if strings.Contains(t, "direct") || strings.Contains(t, "nonstop") {
		p.flight = "direct"
	}
	if strings.Contains(t, "business") || strings.Contains(t, "first class") {
		p.flight = "business"
	}
	if strings.Contains(t, "cheapest") || strings.Contains(t, "lowest price") {
		p.flight = "cheapest"
	}
	return p
}

func scoreFlight(it map[string]any, p prefs, remaining int) int {
	price := num(it["price"])
	if price > remaining {
		return -2000
	}
	switch p.flight {
	case "cheapest":
		return -price
	case "direct":
		if num(it["stops"]) == 0 {
			return 100 - price/100
		}
		return -price / 50
	case "business":
		if strings.Contains(strings.ToLower(str(it["cabin"])), "business") {
			return 100 - price/100
		}
		return -price / 50
	}
	s := 0
	stops := num(it["stops"])
	if b, ok := it["refundable"].(bool); ok && b {
		s += 30
	}
	s -= stops * 20
	s -= price / 100
	return s
}

func scoreStay(it map[string]any, p prefs, remaining int) int {
	s := int(fnum(it["rating"]) * 20)
	hay := strings.ToLower(str(it["name"]) + " " + str(it["type"]) + " " + str(it["neighborhood"]))
	for _, kw := range p.stay {
		if strings.Contains(hay, kw) {
			s += 100
		}
	}
	price := num(it["price"])
	s -= price / 60
	if price > remaining {
		s -= 2000
	}
	return s
}

func scoreDine(it map[string]any, p prefs, remaining int) int {
	s := int(fnum(it["rating"]) * 10)
	if p.greatFood {
		s += int(fnum(it["rating"]) * 10)
	}
	hay := strings.ToLower(str(it["name"]) + " " + str(it["cuisine"]) + " " + str(it["neighborhood"]))
	for _, kw := range p.dine {
		if strings.Contains(hay, kw) {
			s += 100
		}
	}
	price := num(it["price"])
	s -= price / 60
	if price > remaining {
		s -= 2000
	}
	return s
}

func scoreExperience(it map[string]any, p prefs, remaining int) int {
	s := int(fnum(it["rating"]) * 10)
	if p.memorable {
		s += int(fnum(it["rating"]) * 10)
	}
	hay := strings.ToLower(str(it["name"]) + " " + str(it["category"]) + " " + str(it["duration"]))
	for _, kw := range p.experience {
		if strings.Contains(hay, kw) {
			s += 100
		}
	}
	price := num(it["price"])
	s -= price / 60
	if price > remaining {
		s -= 2000
	}
	return s
}

func scoreRide(it map[string]any, p prefs, remaining int) int {
	t := strings.ToLower(str(it["type"]))
	switch p.typ {
	case "cheap":
		if strings.Contains(t, "share") {
			return 100
		}
		return 0
	case "luxury":
		if strings.Contains(t, "premium") {
			return 100
		}
		return 0
	case "private":
		if strings.Contains(t, "private") {
			return 100
		}
		return 0
	}
	if strings.Contains(t, "private") {
		return 50
	}
	if strings.Contains(t, "premium") {
		return 30
	}
	if strings.Contains(t, "share") {
		return 10
	}
	return 0
}

func finalText(res map[string]map[string]any) string {
	if len(res) == 0 {
		return "I searched across the connected services but couldn't finalise any bookings. Please try again."
	}
	var b strings.Builder
	b.WriteString("Your trip is booked! Summary:\n")
	total := 0
	for _, c := range cats {
		if w, ok := res[c.key]; ok {
			b.WriteString("• " + c.titleWrapFn(w) + " — $" + strconv.Itoa(num(w["price"])) + "\n")
			total += num(w["price"])
		}
	}
	b.WriteString("\nTotal: $" + strconv.Itoa(total))
	return b.String()
}

// ---------------------------------------------------------------------------
// LLM mode (OpenAI-compatible chat completions)
// ---------------------------------------------------------------------------

type openAITool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

func (s *server) llmStep(req agentRequest) agentResponse {
	sys := "You are Concierge, an AI travel agent that plans and books whole trips by calling WebMCP tools exposed by independent travel websites. " +
		"Today is " + time.Now().Format("2006-01-02") + ". " +
		"Use ONLY the tools provided. Decide which tool to call next; never invent a result you did not receive from a tool. " +
		"Parse the user's request for: origin, destination, dates, number of travelers, budget, and preferences (e.g. boutique stay, great food, a memorable experience, airport transfer, cheapest flight). " +
		"If dates or traveler count are missing, assume 2 travelers and a 5-day trip starting 7 days from today, and note the assumption in your summary. " +
		"For flights use IATA airport codes (e.g. Lagos = LOS, Accra = ACC); for stays/dining/experiences use the city name. If the user names a country, use its capital city and airport. " +
		"Search across the providers (flights, stay, dinner, experience, ride), pick options that fit the budget and preferences, then book them. " +
		"Human approval for larger purchases is handled automatically by the host — just issue the booking tool calls. " +
		"When everything is booked, end with a concise summary of the trip. Do not call a tool twice if it already succeeded."

	tools := make([]openAITool, 0, len(req.Tools))
	for _, t := range req.Tools {
		ot := openAITool{Type: "function"}
		ot.Function.Name = t.Name
		ot.Function.Description = t.Description
		if len(t.InputSchema) == 0 {
			ot.Function.Parameters = json.RawMessage(`{"type":"object","properties":{}}`)
		} else {
			ot.Function.Parameters = t.InputSchema
		}
		tools = append(tools, ot)
	}

	msgs := make([]message, 0, len(req.Messages)+1)
	msgs = append(msgs, message{Role: "system", Content: sys})
	msgs = append(msgs, req.Messages...)

	body := map[string]any{
		"model":       s.model,
		"messages":    msgs,
		"tools":       tools,
		"temperature": 0.2,
	}
	payload, _ := json.Marshal(body)

	httpReq, err := http.NewRequest("POST", s.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return agentResponse{Type: "error", Text: "failed to build request"}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+s.apiKey)

	client := &http.Client{Timeout: 60 * time.Second}
	httpResp, err := client.Do(httpReq)
	if err != nil {
		return agentResponse{Type: "error", Text: "model request failed: " + err.Error()}
	}
	defer httpResp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(httpResp.Body, 4<<20))

	var parsed struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return agentResponse{Type: "error", Text: "bad model response"}
	}
	if parsed.Error != nil {
		return agentResponse{Type: "error", Text: "model error: " + parsed.Error.Message}
	}
	if len(parsed.Choices) == 0 {
		return agentResponse{Type: "error", Text: "model returned no choices"}
	}
	msg := parsed.Choices[0].Message
	if len(msg.ToolCalls) > 0 {
		tc := msg.ToolCalls[0]
		var args map[string]any
		_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
		if args == nil {
			args = map[string]any{}
		}
		return agentResponse{Type: "tool_call", Name: tc.Function.Name, Arguments: args}
	}
	return agentResponse{Type: "final", Text: strings.TrimSpace(msg.Content)}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

