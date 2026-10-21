#!/usr/bin/env bash
# End-to-end smoke test of the compose deployment. Always cleans up (down -v).
set -euo pipefail
cd "$(dirname "$0")/.."

PORT="${KEYSTONE_PORT:-18080}"
export KEYSTONE_PORT="$PORT"
BASE="http://127.0.0.1:${PORT}"

cleanup() {
  status=$?
  if [ "$status" -ne 0 ]; then docker compose logs --no-color --tail=60 || true; fi
  docker compose down -v --remove-orphans >/dev/null 2>&1 || true
  exit "$status"
}
trap cleanup EXIT

fail() { echo "SMOKE FAIL: $*" >&2; exit 1; }

docker compose up -d --build

echo "waiting for $BASE/api/healthz ..."
ok=""
for _ in $(seq 1 90); do
  if [ "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/api/healthz" || true)" = "200" ]; then ok=1; break; fi
  sleep 2
done
[ -n "$ok" ] || fail "healthz never returned 200"

status_body="$(curl -fsS "$BASE/api/setup/status")"
echo "$status_body" | grep -q '"needed":true' || fail "setup/status: $status_body"

docker compose logs core 2>&1 | grep -q "Keystone setup code: " || fail "no setup code in core logs"

headers="$(curl -fsS -D - -o /dev/null "$BASE/")"
echo "$headers" | grep -qi '^content-security-policy:' || fail "missing Content-Security-Policy"
echo "$headers" | grep -qi '^x-content-type-options:' || fail "missing X-Content-Type-Options"

echo "SMOKE PASS"
