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
# Targets Homarr 2.x. Its onboarding differs from 1.x in three ways that this
# script has to honour:
#
#   * every mutation is claim-gated. POST /api/onboarding/claim issues a
#     `homarr-onboarding-claim` cookie, and the claim stops authorising as soon
#     as the first user exists — so the admin signs in mid-walk and the rest of
#     the steps ride on that session.
#   * the steps collapsed to start -> user -> group -> setup -> finish.
#   * serverSettings.initSettings and onboard.setupIntegrations are gone,
#     replaced by onboard.completeSetup, which needs a real payload.
#
# Usage:
#   eval "$(scripts/bootstrap-test-homarr.sh)"     # exports HOMARR_URL/HOMARR_API_KEY
#   scripts/bootstrap-test-homarr.sh --teardown    # removes the container
#
# Everything it creates is disposable; never point this at a real instance.
set -euo pipefail

CONTAINER="${HOMARR_TEST_CONTAINER:-homarr-tfprovider-test}"
IMAGE="${HOMARR_TEST_IMAGE:-ghcr.io/homarr-labs/homarr:v2.3.0}"
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

# The container is reused by name, so a leftover one built from another image
# would be onboarded and tested in place of $IMAGE -- silently, and with a green
# result. Recreate it whenever the image does not match.
if docker inspect "$CONTAINER" >/dev/null 2>&1; then
  existing="$(docker inspect -f '{{.Config.Image}}' "$CONTAINER")"
  if [[ "$existing" != "$IMAGE" ]]; then
    log "replacing $CONTAINER: it was created from $existing, not $IMAGE"
    docker rm -f "$CONTAINER" >/dev/null
  fi
fi

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

# One cookie jar carries the onboarding claim first and the admin session after.
COOKIES="$(mktemp)"
trap 'rm -f "$COOKIES"' EXIT

trpc() {
  curl -fsS -m 30 -b "$COOKIES" -c "$COOKIES" -X POST "$BASE/api/trpc/$1" \
    -H 'Content-Type: application/json' \
    -d "$2" >/dev/null
}

current_step() {
  curl -fsS -m 30 -b "$COOKIES" -c "$COOKIES" "$BASE/api/trpc/onboard.currentStep" |
    python3 -c 'import json,sys; print(json.load(sys.stdin)["result"]["data"]["json"]["current"])'
}

sign_in() {
  CSRF="$(curl -fsS -b "$COOKIES" -c "$COOKIES" "$BASE/api/auth/csrf" |
    python3 -c 'import json,sys; print(json.load(sys.stdin)["csrfToken"])')"
  curl -fsS -b "$COOKIES" -c "$COOKIES" -o /dev/null \
    -X POST "$BASE/api/auth/callback/credentials" \
    -H 'Content-Type: application/x-www-form-urlencoded' \
    --data-urlencode "csrfToken=$CSRF" \
    --data-urlencode "name=$USERNAME" \
    --data-urlencode "password=$PASSWORD" \
    --data-urlencode "callbackUrl=$BASE" \
    --data-urlencode "json=true"
}

# Claim the onboarding session. 409 means it is already finished, which is fine
# when re-running against a container that was onboarded earlier.
claim_status="$(curl -fsS -o /dev/null -w '%{http_code}' -m 30 \
  -b "$COOKIES" -c "$COOKIES" -X POST "$BASE/api/onboarding/claim" 2>/dev/null || true)"
log "onboarding claim: HTTP ${claim_status:-none}"

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

# From here the claim cookie no longer authorises anything: a user now exists,
# so the remaining steps need a real admin session.
log "signing in as $USERNAME"
sign_in

if [[ "$step" == "group" ]]; then
  trpc onboard.nextStep '{"json":{}}'
  step="$(current_step)"
fi

if [[ "$step" == "setup" ]]; then
  log "completing setup"
  trpc onboard.completeSetup '{"json":{"server":{"defaultLocale":"en","defaultColorScheme":"auto","analyticsEnabled":false},"board":{"name":"home","primaryColor":"#fa5252","secondaryColor":"#fd7e14","itemRadius":"lg"}}}'
  step="$(current_step)"
fi

log "onboarding finished at step: $step"
if [[ "$step" != "finish" ]]; then
  log "onboarding did not reach \"finish\"; container logs follow"
  docker logs --tail 50 "$CONTAINER" >&2
  exit 1
fi

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
