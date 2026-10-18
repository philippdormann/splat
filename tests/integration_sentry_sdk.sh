#!/usr/bin/env bash
# End-to-end integration test: starts a real errgotrack server against a
# scratch database, creates a project through the actual HTTP dashboard,
# and sends a real exception through the official `sentry-sdk` Python
# package (not a hand-crafted payload) - then checks the exception shows up
# in the issue list and detail page.
#
# Requires: go, python3, a `sentry-sdk` install reachable as $PYTHON
# (defaults to creating a throwaway venv under $WORKDIR).
#
# Usage: tests/integration_sentry_sdk.sh
set -euo pipefail
cd "$(dirname "$0")/.."

WORKDIR=$(mktemp -d)
trap 'kill $SERVER_PID 2>/dev/null || true; rm -rf "$WORKDIR"' EXIT

PORT=18099
BASE="http://localhost:$PORT"
ADMIN_USER=admin
ADMIN_PASS=integration-test-password

if [ -z "${PYTHON:-}" ]; then
  python3 -m venv "$WORKDIR/venv"
  "$WORKDIR/venv/bin/pip" install -q sentry-sdk
  PYTHON="$WORKDIR/venv/bin/python"
fi

ERRGOTRACK_SESSION_SECRET=$(openssl rand -hex 32) \
ERRGOTRACK_ADMIN_USERNAME=$ADMIN_USER \
ERRGOTRACK_ADMIN_PASSWORD=$ADMIN_PASS \
ERRGOTRACK_PUBLIC_URL=$BASE \
ERRGOTRACK_DB_PATH="$WORKDIR/errgotrack.db" \
ERRGOTRACK_ADDR=":$PORT" \
go run ./cmd/server > "$WORKDIR/server.log" 2>&1 &
SERVER_PID=$!

for i in $(seq 1 30); do
  if curl -fs "$BASE/healthz" > /dev/null 2>&1; then break; fi
  sleep 0.5
done
curl -fs "$BASE/healthz" > /dev/null || { echo "server did not start"; cat "$WORKDIR/server.log"; exit 1; }

COOKIES="$WORKDIR/cookies.txt"
curl -s -c "$COOKIES" "$BASE/login" -o "$WORKDIR/login.html"
CSRF=$(grep -o 'csrf_token" value="[^"]*"' "$WORKDIR/login.html" | head -1 | sed 's/.*value="//;s/"$//')
curl -s -b "$COOKIES" -c "$COOKIES" -X POST "$BASE/login" \
  -d "csrf_token=$CSRF&username=$ADMIN_USER&password=$ADMIN_PASS" -o /dev/null -w "login status: %{http_code}\n"

curl -s -b "$COOKIES" "$BASE/projects/new" -o "$WORKDIR/new.html"
CSRF2=$(grep -o 'csrf_token" value="[^"]*"' "$WORKDIR/new.html" | head -1 | sed 's/.*value="//;s/"$//')
curl -s -b "$COOKIES" -c "$COOKIES" -X POST "$BASE/projects/new" \
  -d "csrf_token=$CSRF2&name=Integration+Test&slug=integration-test&default_environment=production&retention_days=90" \
  -o /dev/null -w "project create status: %{http_code}\n"

curl -s -b "$COOKIES" "$BASE/projects/1" -o "$WORKDIR/proj.html"
DSN=$(grep -o 'word-break:break-all">[^<]*' "$WORKDIR/proj.html" | sed 's/.*">//')
echo "DSN: $DSN"

SENTRY_DSN="$DSN" "$PYTHON" - <<'PYEOF'
import os
import sentry_sdk

sentry_sdk.init(
    dsn=os.environ["SENTRY_DSN"],
    environment="production",
    release="integration-test@1.0.0",
    send_default_pii=False,
    traces_sample_rate=0.0,
)

def divide(a, b):
    return a / b

try:
    divide(10, 0)
except ZeroDivisionError:
    sentry_sdk.capture_exception()

sentry_sdk.flush(timeout=5)
PYEOF

sleep 1
curl -s -b "$COOKIES" "$BASE/issues/1" -o "$WORKDIR/detail.html"

if ! grep -q "ZeroDivisionError" "$WORKDIR/detail.html"; then
  echo "FAIL: exception type not found in issue detail page"
  exit 1
fi
if ! grep -q "division by zero" "$WORKDIR/detail.html"; then
  echo "FAIL: exception message not found in issue detail page"
  exit 1
fi
if ! grep -q "divide" "$WORKDIR/detail.html"; then
  echo "FAIL: stack frame function name not found in issue detail page"
  exit 1
fi

echo "PASS: real sentry-sdk exception was ingested and is displayed correctly"
