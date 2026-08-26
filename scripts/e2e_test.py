#!/usr/bin/env python3
"""End-to-end integration test for the Concierge ecosystem.

Drives the Concierge agent endpoint against the real provider HTTP APIs,
exercising the deterministic planner, the golden trip, and the rejection
fallback. This tests everything except the in-browser document.modelContext
calls (use the `?autopilot=1` page for that).

Run with all six apps started (see scripts/dev.sh).
"""

import json
import sys
import urllib.request

BASE = "http://localhost:8080"

# Tool name -> provider origin. (WebMCP tools are origin-scoped.)
TOOL_ORIGIN = {
    # FlyNow
    "search_flights": "http://localhost:8081",
    "get_flight_details": "http://localhost:8081",
    "check_flight_availability": "http://localhost:8081",
    "hold_flight": "http://localhost:8081",
    "book_flight": "http://localhost:8081",
    "cancel_flight": "http://localhost:8081",
    # Stayly
    "search_stays": "http://localhost:8082",
    "get_stay_details": "http://localhost:8082",
    "check_room_availability": "http://localhost:8082",
    "reserve_room": "http://localhost:8082",
    "modify_reservation": "http://localhost:8082",
    # Dine
    "search_restaurants": "http://localhost:8083",
    "get_restaurant_details": "http://localhost:8083",
    "check_table_availability": "http://localhost:8083",
    "book_table": "http://localhost:8083",
    # Explore
    "search_experiences": "http://localhost:8084",
    "get_experience_details": "http://localhost:8084",
    "check_experience_availability": "http://localhost:8084",
    "book_experience": "http://localhost:8084",
    "cancel_booking": "http://localhost:8084",
    # Ride
    "search_rides": "http://localhost:8085",
    "get_ride_quote": "http://localhost:8085",
    "book_ride": "http://localhost:8085",
    "modify_ride": "http://localhost:8085",
    "cancel_ride": "http://localhost:8085",
}

PROMPT = ("Plan a five-day trip to Paris for two people under $3,500. "
          "Automatically book anything under $100 and ask me before larger purchases.")


def post(url, body):
    req = urllib.request.Request(
        url, data=json.dumps(body).encode(), headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req) as r:
        return json.loads(r.read().decode())


def agent(messages):
    tools = [{
        "name": n, "description": "", "inputSchema": {"type": "object", "properties": {}},
        "origin": TOOL_ORIGIN[n], "annotations": {"readOnlyHint": False},
    } for n in TOOL_ORIGIN]
    return post(BASE + "/api/agent", {"tools": tools, "messages": messages,
                                      "config": {"budget": 3500, "threshold": 100}})


def drive(reject_flight, label):
    print(f"\n=== {label} ===")
    messages = [{"role": "user", "content": PROMPT}]
    seq = 0
    for _ in range(30):
        r = agent(messages)
        if r["type"] == "final":
            print("FINAL:", r["text"].replace("\n", " | "))
            return r["text"]
        if r["type"] != "tool_call":
            print("UNEXPECTED RESPONSE:", r)
            return None
        name, args = r["name"], r["arguments"]
        seq += 1
        call_id = "c%d" % seq
        assistant = {"role": "assistant", "content": None, "tool_calls": [
            {"id": call_id, "type": "function",
             "function": {"name": name, "arguments": json.dumps(args)}}]}

        if reject_flight and name == "book_flight" and seq == 6:
            messages += [assistant, {"role": "tool", "tool_call_id": call_id,
                                     "content": json.dumps(
                                         {"error": "rejected by user", "tool": name, "arguments": args})}]
            print(f"  [{seq}] {name} -> REJECTED BY USER")
            continue

        res = post(TOOL_ORIGIN[name] + "/api/" + name, args)
        messages += [assistant, {"role": "tool", "tool_call_id": call_id,
                                 "content": json.dumps(res)}]
        key = next(iter(res), "?")
        print(f"  [{seq}] {name} -> {key}")
    print("LOOP EXCEEDED 30 STEPS")
    return None


def main():
    golden = drive(False, "GOLDEN TRIP (expect 5 bookings)")
    fallback = drive(True, "REJECTION (expect flight fallback to KLM)")

    required = ["Air France", "Hotel Lumière", "Le Jardin", "Seine Evening Cruise", "Private transfer"]
    ok = all(x in golden for x in required) and ("KLM" in fallback)
    print("\n" + ("✅ PASS" if ok else "❌ FAIL"))
    sys.exit(0 if ok else 1)


if __name__ == "__main__":
    main()
