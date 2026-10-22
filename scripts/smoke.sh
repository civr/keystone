#!/usr/bin/env bash
# End-to-end smoke test of the compose deployment. Always cleans up (down -v).
set -euo pipefail
cd "$(dirname "$0")/.."

PORT="${KEYSTONE_PORT:-18080}"
export KEYSTONE_PORT="$PORT"
export KEYSTONE_GATE_PORT="${KEYSTONE_GATE_PORT:-18081}" KEYSTONE_LDAPS_PORT="${KEYSTONE_LDAPS_PORT:-18636}"
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

# The shell (embedded in core) must reference assets that web actually
# serves: both images are built separately from the same KEYSTONE_VERSION.
html="$(curl -fsS "$BASE/")"
assets="$(echo "$html" | grep -oE '/_app/immutable/[^"'"'"' ]+\.js' | sort -u)"
[ -n "$assets" ] || fail "no /_app/immutable/*.js referenced from GET /"
for a in $assets; do
  code="$(curl -s -o /dev/null -w '%{http_code}' "$BASE$a")"
  [ "$code" = "200" ] || fail "asset $a referenced by the shell returned $code"
done
curl -fsS -D - -o /dev/null "$BASE$(echo "$assets" | head -1)" | grep -qi '^cache-control: .*immutable' \
  || fail "immutable asset without immutable Cache-Control"
[ "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/_app/version.json")" = "200" ] || fail "/_app/version.json not served"
echo "shell assets OK: $(echo "$assets" | wc -l | tr -d ' ') files"

GATE="http://127.0.0.1:${KEYSTONE_GATE_PORT:-18081}"
for _ in $(seq 1 30); do
  [ "$(curl -s -o /dev/null -w '%{http_code}' "$GATE/healthz" || true)" = "200" ] && break
  sleep 2
done
code="$(curl -s -o /dev/null -w '%{http_code}' -H 'X-Forwarded-Proto: https' -H 'X-Forwarded-Host: nope.home.lan' -H 'X-Forwarded-Uri: /' "$GATE/verify")"
[ "$code" = "403" ] || fail "gate /verify for an unknown app returned $code"
(exec 3<>"/dev/tcp/127.0.0.1/${KEYSTONE_LDAPS_PORT:-18636}") 2>/dev/null || fail "LDAPS port not open"
echo "gate and ldap OK"

echo "SMOKE PASS"
