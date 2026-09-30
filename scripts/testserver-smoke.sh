#!/usr/bin/env bash
# Delete-path smoke test against the compose test server.
# Usage: scripts/testserver-smoke.sh [base_url]   (COMPOSE overrides the compose command)
set -euo pipefail

BASE="${1:-http://localhost:8123}"
if [ -z "${COMPOSE:-}" ]; then
  if command -v podman >/dev/null 2>&1; then COMPOSE="podman compose"; else COMPOSE="docker compose"; fi
fi
TS="$(date +%s)"
BODY="$(mktemp)"
trap 'rm -f "$BODY"' EXIT

# call <json> [path-suffix] -> prints HTTP status, body in $BODY
call() {
  curl -s -o "$BODY" -w '%{http_code}' -X POST "$BASE/public/departments${2:-}" \
    -H 'Content-Type: application/json' -d "$1"
}
expect() { # name want got
  if [ "$2" != "$3" ]; then echo "FAIL $1: want $2 got $3: $(cat "$BODY")"; exit 1; fi
  echo "ok   $1 ($3)"
}
ids() { grep -o '"id":[0-9]*' "$BODY" | cut -d: -f2; }

expect create 200 "$(call "{\"operation\":\"create\",\"data\":{\"name\":\"Smoke\",\"code\":\"S$TS\"}}")"
ID="$(ids | head -1)"
expect read 200 "$(call '{"operation":"read"}' "/$ID")"
expect update 200 "$(call '{"operation":"update","data":{"name":"Smoke2"}}' "/$ID")"
expect delete 200 "$(call '{"operation":"delete"}' "/$ID")"
expect delete-again 404 "$(call '{"operation":"delete"}' "/$ID")"

expect batch-create 200 "$(call "{\"operation\":\"create\",\"data\":[{\"name\":\"B\",\"code\":\"B1$TS\"},{\"name\":\"B\",\"code\":\"B2$TS\"}]}")"
B1="$(ids | sed -n 1p)"; B2="$(ids | sed -n 2p)"
expect batch-delete 200 "$(call "{\"operation\":\"delete\",\"data\":[\"$B1\",\"$B2\"]}")"

echo "--- dbtrace"
$COMPOSE logs testserver 2>&1 | grep 'dbtrace' | tail -20 || true
