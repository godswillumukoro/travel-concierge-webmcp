#!/usr/bin/env bash
# Start every app in the Concierge ecosystem, each on its own origin.
# Each app is an independent, self-hostable Go service (no shared code).
#
#   ./scripts/dev.sh        # start all, Ctrl+C stops all
#   ./scripts/stop.sh       # stop all (including orphaned go-run children)
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
PORTS=(8080 8081 8082 8083 8084 8085)

# name:port — providers first, then the host.
APPS=(
  "flynow:8081"
  "stayly:8082"
  "dine:8083"
  "explore:8084"
  "ride:8085"
  "concierge:8080"
)

pids=()
cleanup() {
  echo ""
  echo "Stopping all services…"
  for pid in "${pids[@]}"; do kill "$pid" 2>/dev/null || true; done
  for port in "${PORTS[@]}"; do
    lsof -tiTCP:"$port" -sTCP:LISTEN 2>/dev/null | xargs kill -9 2>/dev/null || true
  done
}
trap cleanup EXIT INT TERM

for entry in "${APPS[@]}"; do
  app="${entry%%:*}"
  port="${entry##*:}"
  echo "▶ starting $app on http://localhost:$port"
  (cd "$ROOT/$app" && PORT="$port" go run .) &
  pids+=($!)
done

echo ""
echo "All services starting. Concierge: http://localhost:8080"
echo "Remember: launch Chrome with --enable-features=WebMCP"
wait
