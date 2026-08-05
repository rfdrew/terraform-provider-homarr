#!/usr/bin/env bash
#
# Boots a throwaway Homarr instance and prints an admin API key on stdout.
#
# Homarr requires a one-time onboarding walk before its API is usable: an admin
# account has to exist, and the onboarding state machine has to reach "finish".
# None of those steps are exposed over the REST API, so this script drives them
# through the tRPC endpoints the UI itself uses, then signs in with NextAuth and
# mints an API key.
#
# Usage:
#   eval "$(scripts/bootstrap-test-homarr.sh)"     # exports HOMARR_URL/HOMARR_API_KEY
#   scripts/bootstrap-test-homarr.sh --teardown    # removes the container
#
# Everything it creates is disposable; never point this at a real instance.
set -euo pipefail

CONTAINER="${HOMARR_TEST_CONTAINER:-homarr-tfprovider-test}"
IMAGE="${HOMARR_TEST_IMAGE:-ghcr.io/homarr-labs/homarr:v1.73.0}"
PORT="${HOMARR_TEST_PORT:-7575}"
BASE="http://localhost:${PORT}"
USERNAME="tfadmin"
PASSWORD="TfProvider!23"

if [[ "${1:-}" == "--teardown" ]]; then
  docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
  echo "removed $CONTAINER" >&2
  exit 0
fi

log() { echo "$*" >&2; }

if ! docker inspect "$CONTAINER" >/dev/null 2>&1; then
  log "starting $IMAGE as $CONTAINER on port $PORT"
  docker run -d --name "$CONTAINER" \
    -p "${PORT}:7575" \
    -e SECRET_ENCRYPTION_KEY=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef \
    -e AUTH_PROVIDERS=credentials \
    "$IMAGE" >/dev/null
fi

log "waiting for Homarr to become healthy"
for _ in $(seq 1 60); do
  if [[ "$(curl -fsS -o /dev/null -w '%{http_code}' -m 5 "$BASE/api/health/live" 2>/dev/null || true)" == "200" ]]; then
    break
  fi
  sleep 3
done
if [[ "$(curl -fsS -o /dev/null -w '%{http_code}' -m 5 "$BASE/api/health/live" 2>/dev/null || true)" != "200" ]]; then
  log "Homarr did not become healthy in time; container logs follow"
  docker logs --tail 50 "$CONTAINER" >&2
  exit 1
fi

trpc() {
  curl -fsS -m 30 -X POST "$BASE/api/trpc/$1" \
    -H 'Content-Type: application/json' \
    -d "$2" >/dev/null
}

current_step() {
  curl -fsS -m 30 "$BASE/api/trpc/onboard.currentStep" |
    python3 -c 'import json,sys; print(json.load(sys.stdin)["result"]["data"]["json"]["current"])'
}

# The onboarding steps are a state machine; each mutation advances it. Skip any
# step the instance has already passed so the script is safe to re-run.
step="$(current_step)"
log "onboarding step: $step"

if [[ "$step" == "start" ]]; then
  trpc onboard.nextStep '{"json":{}}'
  step="$(current_step)"
fi

if [[ "$step" == "user" ]]; then
  log "creating admin $USERNAME"
  trpc user.initUser "$(printf '{"json":{"username":"%s","password":"%s","confirmPassword":"%s","email":"%s@example.com"}}' \
    "$USERNAME" "$PASSWORD" "$PASSWORD" "$USERNAME")"
  step="$(current_step)"
fi

if [[ "$step" == "settings" ]]; then
  trpc serverSettings.initSettings \
    '{"json":{"analytics":{"enableGeneral":false},"crawlingAndIndexing":{"noIndex":true,"noFollow":true,"noTranslate":true,"noSiteLinksSearchBox":true}}}'
  step="$(current_step)"
fi

if [[ "$step" == "integrations" ]]; then
  trpc onboard.setupIntegrations '{"json":{}}'
  step="$(current_step)"
fi

log "onboarding finished at step: $step"

# API keys can only be minted by an authenticated session, so sign in through
# NextAuth's credentials callback and keep the session cookie.
COOKIES="$(mktemp)"
trap 'rm -f "$COOKIES"' EXIT

CSRF="$(curl -fsS -c "$COOKIES" "$BASE/api/auth/csrf" |
  python3 -c 'import json,sys; print(json.load(sys.stdin)["csrfToken"])')"

curl -fsS -b "$COOKIES" -c "$COOKIES" -o /dev/null \
  -X POST "$BASE/api/auth/callback/credentials" \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  --data-urlencode "csrfToken=$CSRF" \
  --data-urlencode "name=$USERNAME" \
  --data-urlencode "password=$PASSWORD" \
  --data-urlencode "callbackUrl=$BASE" \
  --data-urlencode "json=true"

API_KEY="$(curl -fsS -b "$COOKIES" -X POST "$BASE/api/trpc/apiKeys.create" \
  -H 'Content-Type: application/json' -d '{"json":{}}' |
  python3 -c 'import json,sys; print(json.load(sys.stdin)["result"]["data"]["json"]["apiKey"])')"

if [[ -z "$API_KEY" ]]; then
  log "failed to create an API key"
  exit 1
fi

log "API key created"
echo "export HOMARR_URL=$BASE"
echo "export HOMARR_API_KEY=$API_KEY"
