#!/usr/bin/env bash
# Stop every app in the Concierge ecosystem (by port, which also catches any
# orphaned `go run` child processes).
set -euo pipefail

PORTS=(8080 8081 8082 8083 8084 8085)

for port in "${PORTS[@]}"; do
  pids="$(lsof -tiTCP:"$port" -sTCP:LISTEN 2>/dev/null || true)"
  if [ -n "$pids" ]; then
    echo "▶ stopping :$port (pids $pids)"
    echo "$pids" | xargs kill 2>/dev/null || true
  fi
done

sleep 1
# force-kill anything still lingering
for port in "${PORTS[@]}"; do
  lsof -tiTCP:"$port" -sTCP:LISTEN 2>/dev/null | xargs kill -9 2>/dev/null || true
done

echo "✓ all Concierge services stopped"
