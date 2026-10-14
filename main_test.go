package main

import "testing"

func TestFlightSearchToBooking(t *testing.T) {
	// Replay provider search results through the real planner, without network calls.
	req := agentRequest{Messages: []message{{Role: "user", Content: "Find the cheapest flight to Paris"}}}
	req.Config.Budget = 3500
	for _, c := range cats {
		var call toolCall
		call.ID = c.searchTool
		call.Function.Name = c.searchTool
		call.Function.Arguments = "{}"
		result := `{}`
		if c.key == "flight" {
			result = `{"flights":[{"id":"FL-001","price":842,"stops":0}]}`
		}
		req.Messages = append(req.Messages,
			message{Role: "assistant", ToolCalls: []toolCall{call}},
			message{Role: "tool", ToolCallID: call.ID, Content: result},
		)
	}
	got := plannerStep(req)
	if got.Name != "book_flight" {
		t.Fatalf("expected book_flight after search, got %q", got.Name)
	}
	if got.Arguments["flight_id"] != "FL-001" {
		t.Fatalf("booking lost the selected flight: want flight_id=FL-001, got %v", got.Arguments["flight_id"])
	}
}
