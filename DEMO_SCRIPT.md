# Concierge — Demo Script

> **Your trip. One conversation.** — Concierge plans and books an entire trip by
> directly using the WebMCP tools exposed by independent travel websites, backed
> by live data from real APIs.

Target length: **~3 minutes** (core) + ~90 seconds (optional disruption).

---

## Pre-recording checklist

1. Start everything:
   ```bash
   cd /Users/gogo/code/concierge && ./scripts/dev.sh
   # or: cd /Users/gogo/code && docker compose up --build
   ```
2. Launch Chrome **fresh** (the flag is required):
   ```bash
   "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
     --enable-features=WebMCP --user-data-dir=/tmp/concierge-demo
   ```
   A fresh `--user-data-dir` avoids a dirty profile and guarantees clean state.
3. Open **http://localhost:8080**. Confirm the "Connected services" panel shows
   **5/5 services · 27 tools** with a green dot on each provider.
4. Restart the apps right before recording so all booking state is empty.
5. Hide any API-key prompts; the demo runs fully on mock + Photon/OSRM data
   (no keys required).

---

## Core demo (~3:00)

### 0:00–0:20 — The problem

**Voiceover:**
> "Travel websites weren't built to work together. You juggle flights, hotels,
> dinner, experiences and rides across five tabs — copying dates, addresses and
> booking numbers by hand. You are the integration layer."

**Screen:** Show the five provider sites in separate tabs or side-by-side —
FlyNow, Stayly, Dine, Explore, Ride — each open on its own `localhost` origin.

---

### 0:20–0:45 — The idea

**Voiceover:**
> "Concierge turns them into an agent-native travel ecosystem. Each site exposes
> its capabilities through WebMCP — `document.modelContext` — and Concierge, a
> top-level web app, discovers those tools across origins and executes them.
> Real APIs — Duffel, Amadeus, Photon, OSRM — make the data live."

**Screen:** Open **http://localhost:8080**. Point at the header, then click
**⚙ Services**. Show the five providers, each with a green dot, a tool count,
and a **"Live data: …"** badge. Click **FlyNow** to expand its 6 tools with
`read` / `write` badges.

**Voiceover:**
> "Clicking a service reveals the exact WebMCP tools it exposes — search and read
> tools, and write tools like `book_flight` — with read/write annotations."

---

### 0:45–1:45 — The killer prompt

**Screen:** Back to the chat. Click the quick prompt **"Plan my trip to Paris"**
(or type it):

> *Plan a five-day trip to Paris for two people under $3,500. We want a boutique
> stay, great food, one memorable experience and airport transport. Automatically
> book anything under $100 and ask me before larger purchases.*

**Voiceover:**
> "I give Concierge a goal — not instructions. Watch the activity log: it searches
> FlyNow, Stayly, Dine, Explore and Ride through WebMCP, and reasons about the
> results — a boutique stay, great food, a memorable experience."

**Screen:** Keep the **Agent activity** panel visible. It streams:

```
FlyNow.search_flights(LOS→CDG)      → 5 flights
Stayly.search_stays(Paris)          → real hotels
Dine.search_restaurants(Paris)      → real restaurants
Explore.search_experiences(Paris)   → real museums
Ride.search_rides(CDG→hotel)        → real route, live fare
```

---

### 1:45–2:15 — Human approval

**Voiceover:**
> "It found the trip. Now the human stays in control — anything over $100 needs
> approval."

**Screen:** An approval card appears: **FlyNow · Air France · $842**. Click
**Approve**. A second card: **Stayly · Hotel Lumière · $1,180**. Approve. A
third: **Dine · Le Jardin · $120**. Approve.

**Voiceover:**
> "The $96 Seine cruise and the ~$28 airport transfer were under the threshold,
> so they were booked automatically."

**Screen:** Point at the activity log showing the auto-booked experience and ride.

---

### 2:15–2:45 — The result

**Screen:** The itinerary card renders:

```
✈ Air France AF-104 — $842   (4,718 km, real distance)
🏠 Hotel Lumière — $1,180
🍽 Le Jardin — $120
🎟 Seine Evening Cruise — $96
🚕 Private transfer — $28    (real OSRM route: 27.7 km)
────────────────────────────
Total $2,266 · $1,234 remaining
```

**Voiceover:**
> "One itinerary, assembled across five independent websites — flights, stay,
> dinner, an experience, and the transfer — with live distance and routing where
> the data is real."

---

### 2:45–3:00 — Why WebMCP

**Voiceover:**
> "The web already has the services. WebMCP makes their capabilities available
> to agents. Real APIs make the data live. Concierge puts them together."

**Screen:** Hold on the completed itinerary + the green connected-services panel.

---

## Optional: disruption recovery (+~90s)

**Voiceover:**
> "And because this is a real agent, it can react when plans change."

**Screen:** Click **"⚠ Simulate flight cancellation"** in the trip panel.

**Voiceover:**
> "The flight is cancelled. Concierge rebooks a replacement, shifts the airport
> transfer to the new arrival, and moves dinner later — asking me to approve the
> changes."

**Screen:**
- Activity: `FlyNow.cancel_flight` → cancelled.
- Approval card: **KLM KL-578 · $865** → Approve.
- Approval card: **Le Jardin · 9:30 PM** → Approve.
- Final message: *"…rebooked you on KLM arriving 06:10, updated your transfer to
  06:55, moved dinner to 9:30 PM at Le Jardin."*
- Updated itinerary total **$2,289**.

---

## Optional: preferences (+~30s)

**Screen:** Send *"Plan a trip to Paris but I want the cheapest flight."*

**Voiceover:**
> "Preferences matter too — the planner parses the request and picks the cheapest
> flight, or a luxury ride, or a museum, instead of a hardcoded answer."

---

## Recording tips

- **Clean state:** restart all apps before each take (state is in-memory).
- **Two windows:** keep Concierge in the main window; open the five provider
  sites in a second window for the "problem" intro, then switch.
- **Zoom:** the chat and the activity log are the star — keep them legible.
- **Autopilot for CI/demo:** `?autopilot=1` runs the whole golden trip with
  approvals auto-approved (handy for a hands-free loop or a thumbnail).
- **If WebMCP is missing:** you'll see a banner; confirm Chrome was launched with
  `--enable-features=WebMCP` (or set `ORIGIN_TRIAL_TOKEN`).
