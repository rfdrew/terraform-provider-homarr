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
# On failure the script prints `false` on stdout, so that the eval above fails
# too instead of silently exporting nothing — `eval "$(...)"` otherwise returns
# 0 whatever the script's exit status was.
#
# Everything it creates is disposable; never point this at a real instance.
set -euo pipefail

CONTAINER="${HOMARR_TEST_CONTAINER:-homarr-tfprovider-test}"
IMAGE="${HOMARR_TEST_IMAGE:-ghcr.io/homarr-labs/homarr:v2.3.0}"
PORT="${HOMARR_TEST_PORT:-7575}"
BASE="http://127.0.0.1:${PORT}"
USERNAME="tfadmin"
PASSWORD="TfProvider!23"

# The cookie jar outlives a single run. The onboarding claim is valid for four
# hours and is tied to the token in this jar, so a run that died half-way can be
# resumed by the next one instead of being locked out with a 423.
COOKIES="${TMPDIR:-/tmp}/${CONTAINER}.cookies"

log() { echo "$*" >&2; }

# Traffic to the container must never go through HTTP(S)_PROXY or ALL_PROXY.
curl() { command curl --noproxy '*' "$@"; }

if [[ "${1:-}" == "--teardown" ]]; then
  # -v also removes the image's anonymous /appdata volume.
  docker rm -f -v "$CONTAINER" >/dev/null 2>&1 || true
  rm -f "$COOKIES"
  echo "removed $CONTAINER" >&2
  exit 0
fi

trap 'status=$?; if [[ $status -ne 0 ]]; then echo false; fi' EXIT

# The container is reused by name, so a leftover one built from another image
# would be onboarded and tested in place of $IMAGE -- silently, and with a green
# result. Recreate it whenever the image does not match.
if docker inspect "$CONTAINER" >/dev/null 2>&1; then
  existing="$(docker inspect -f '{{.Config.Image}}' "$CONTAINER")"
  if [[ "$existing" != "$IMAGE" ]]; then
    log "replacing $CONTAINER: it was created from $existing, not $IMAGE"
    docker rm -f -v "$CONTAINER" >/dev/null
    rm -f "$COOKIES"
  fi
fi

# A container that stopped — after a Docker or host restart, say — keeps its
# onboarded database, so it is started again rather than left for the health
# check to time out against.
if [[ "$(docker inspect -f '{{.State.Running}}' "$CONTAINER" 2>/dev/null || true)" == "false" ]]; then
  log "starting the stopped container $CONTAINER"
  docker start "$CONTAINER" >/dev/null
fi

if ! docker inspect "$CONTAINER" >/dev/null 2>&1; then
  log "starting $IMAGE as $CONTAINER on port $PORT"
  rm -f "$COOKIES"
  # NO_EXTERNAL_CONNECTION skips the first-boot download and import of ~11k
  # icons, which can stall the event loop for longer than a health probe waits;
  # Homarr's own end-to-end tests set it for the same reason.
  docker run -d --name "$CONTAINER" \
    -p "127.0.0.1:${PORT}:7575" \
    -e SECRET_ENCRYPTION_KEY=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef \
    -e AUTH_PROVIDERS=credentials \
    -e NO_EXTERNAL_CONNECTION=true \
    "$IMAGE" >/dev/null
fi
touch "$COOKIES"

# /api/health/ready answers as soon as nginx reaches Next.js; /api/health/live
# also checks sqlite and redis, has no server-side timeout, and names the
# failing check in its body. Both the body and curl's own error are kept, so a
# timeout says whether the probe never reached the container (curl error) or
# Homarr itself was unhealthy (HTTP 500 and which check).
log "waiting for Homarr to become healthy"
last=""
healthy=""
for _ in $(seq 1 60); do
  last="$(curl -sS -o /dev/null -w 'ready: HTTP %{http_code}' -m 5 "$BASE/api/health/ready" 2>&1)" || true
  if [[ "$last" == "ready: HTTP 200" ]]; then
    last="$(curl -sS -m 20 -w ' HTTP %{http_code}' "$BASE/api/health/live" 2>&1)" || true
    if [[ "$last" == *" HTTP 200" ]]; then
      healthy=1
      break
    fi
  fi
  sleep 3
done
if [[ -z "$healthy" ]]; then
  log "Homarr did not become healthy in time. Last probe of $BASE: $last"
  log "A curl connection error while the logs below show Next.js ready means this shell cannot"
  log "reach 127.0.0.1:${PORT} (sandbox, firewall or port mapping), not that Homarr is unhealthy."
  log "Container status: $(docker inspect -f '{{.State.Status}} (exit {{.State.ExitCode}})' "$CONTAINER" 2>&1)"
  log "Container logs follow:"
  docker logs --tail 200 "$CONTAINER" >&2
  exit 1
fi

# tRPC answers failures with a JSON body naming the procedure's error (step
# denied, claim missing, validation issues). --fail-with-body keeps it so that it
# can be logged instead of a bare `curl: (22)`.
trpc() {
  local out
  if ! out="$(curl -sS --fail-with-body -m 30 -b "$COOKIES" -c "$COOKIES" -X POST "$BASE/api/trpc/$1" \
    -H 'Content-Type: application/json' -d "$2")"; then
    log "tRPC $1 failed: $out"
    exit 1
  fi
}

current_step() {
  local out
  if ! out="$(curl -sS --fail-with-body -m 30 -b "$COOKIES" -c "$COOKIES" "$BASE/api/trpc/onboard.currentStep")"; then
    log "tRPC onboard.currentStep failed: $out"
    exit 1
  fi
  python3 -c 'import json,sys; print(json.load(sys.stdin)["result"]["data"]["json"]["current"])' <<<"$out"
}

# Auth.js redirects both on success and on a rejected login, so the HTTP status
# of the callback proves nothing. The session endpoint is asked instead whether
# an admin is now signed in.
sign_in() {
  CSRF="$(curl -fsS -b "$COOKIES" -c "$COOKIES" "$BASE/api/auth/csrf" |
    python3 -c 'import json,sys; print(json.load(sys.stdin)["csrfToken"])')"
  curl -sS -b "$COOKIES" -c "$COOKIES" -o /dev/null \
    -X POST "$BASE/api/auth/callback/credentials" \
    -H 'Content-Type: application/x-www-form-urlencoded' \
    --data-urlencode "csrfToken=$CSRF" \
    --data-urlencode "name=$USERNAME" \
    --data-urlencode "password=$PASSWORD" \
    --data-urlencode "callbackUrl=$BASE"
  if ! curl -fsS -b "$COOKIES" "$BASE/api/auth/session" |
    python3 -c 'import json,sys; s=json.load(sys.stdin) or {}; sys.exit(0 if "admin" in s.get("user",{}).get("permissions",[]) else 1)'; then
    log "signing in as $USERNAME did not produce an admin session"
    exit 1
  fi
}

# Claim the onboarding session. 409 means it is already finished, which is fine
# when re-running against a container that was onboarded earlier; 403 means a
# user already exists, and the admin session below takes over from there.
claim_status="$(curl -sS -o /dev/null -w '%{http_code}' -m 30 \
  -b "$COOKIES" -c "$COOKIES" -X POST "$BASE/api/onboarding/claim" 2>/dev/null || true)"
log "onboarding claim: HTTP ${claim_status:-none}"
case "$claim_status" in
  200 | 403 | 409) ;;
  423)
    log "onboarding is claimed by an earlier run whose cookie jar is gone; run: $0 --teardown"
    exit 1
    ;;
  *)
    log "unexpected onboarding claim status: ${claim_status:-none}"
    exit 1
    ;;
esac

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

# Only reachable with LDAP or OIDC enabled: with credentials alone, initUser
# goes straight to "setup". "group" is left by creating the external admin
# group, not by onboard.nextStep, which only advances from "start".
if [[ "$step" == "group" ]]; then
  trpc group.createInitialExternalGroup '{"json":{"name":"tfacc-admins"}}'
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

if ! key_response="$(curl -sS --fail-with-body -b "$COOKIES" -X POST "$BASE/api/trpc/apiKeys.create" \
  -H 'Content-Type: application/json' -d '{"json":{}}')"; then
  log "tRPC apiKeys.create failed: $key_response"
  exit 1
fi
API_KEY="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["result"]["data"]["json"]["apiKey"])' <<<"$key_response")"

if [[ -z "$API_KEY" ]]; then
  log "failed to create an API key"
  exit 1
fi

log "API key created"
echo "export HOMARR_URL=$BASE"
echo "export HOMARR_API_KEY=$API_KEY"
