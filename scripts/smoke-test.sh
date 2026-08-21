#!/usr/bin/env bash
#
# Smoke-test a built askweb image from outside the container.
#
# Everything here is asserted through the image's external interface: the
# published port, MCP over HTTP at /mcp, and the mounted data directory. Nothing
# asserts how the image was built — no base image, no layers, no size, no paths
# inside it — so the Dockerfile stays free to change as long as an operator
# cannot tell.
#
# The four assertions map to the ways packaging breaks while every Go test stays
# green: a wrong port or bind address, a missing certificate store, a whitelist
# mount that is not writable or not persistent, and a data directory only the
# image's own user can write.
#
# The whole set runs twice: once as the image's default user, once under a uid
# and gid that exist nowhere in the image, which is the arrangement an operator
# making `user: "1003:1002"` work is expected to arrange.
#
# Usage: scripts/smoke-test.sh [image-tag]
#
# Needs a Docker daemon, curl, and jq. Reaches https://example.com, because
# proving the certificate store is present is the point of one of the
# assertions and a self-signed host would prove the opposite.

set -euo pipefail

IMAGE="${1:-askweb:smoke}"

# A host the image is told about up front, and one it has to be asked about.
readonly KNOWN_HOST="example.com"
readonly KNOWN_URL="https://example.com/"
readonly KNOWN_MARKER="Example Domain"
readonly APPROVED_HOST="go.dev"
readonly APPROVED_URL="https://go.dev/"
readonly REFUSED_URL="https://example.net/"

# The version a real client negotiates against this server. The SDK serves
# 2026-07-28 only on a stateless handler, so a client asking for it is refused;
# on this version the SDK bridges an input request to a server-initiated
# elicitation/create over the event stream, which is the exchange an operator's
# client actually performs and therefore the one worth testing.
readonly PROTOCOL="2025-06-18"

readonly OVERRIDE_UID=1003
readonly OVERRIDE_GID=1002

workdir="$(mktemp -d)"
container=""

cleanup() {
  if [ -n "$container" ]; then
    docker rm -f "$container" >/dev/null 2>&1 || true
  fi
  chmod -R u+rwX "$workdir" 2>/dev/null || true
  rm -rf "$workdir"
}
trap cleanup EXIT

fail() {
  echo "FAIL: $*" >&2
  if [ -n "$container" ]; then
    echo "--- container logs ---" >&2
    docker logs "$container" >&2 2>&1 || true
  fi
  exit 1
}

pass() { echo "  ok: $*"; }

# --- MCP over HTTP -----------------------------------------------------------
#
# The sequence mirrors the Go suite's TestServesOverStreamableHTTPAtMCPPath:
# initialize, notifications/initialized, then tools/call. A response may come
# back as JSON or as an SSE event depending on the negotiated stream, so the
# body is scanned as text rather than parsed as one fixed shape.

port=""
session=""

mcp_post() {
  curl -sS --max-time 30 \
    -H 'Content-Type: application/json' \
    -H 'Accept: application/json, text/event-stream' \
    -H "MCP-Protocol-Version: $PROTOCOL" \
    ${session:+-H "Mcp-Session-Id: $session"} \
    "http://127.0.0.1:${port}/mcp" -d "$1"
}

# mcp_connect opens a session. With an argument, the client declares that it can
# put questions to a human; without one it cannot, which is what a restarted
# server has to satisfy from the whitelist alone.
mcp_connect() {
  local capabilities='{}'
  if [ "${1:-}" = "can-prompt" ]; then
    capabilities='{"elicitation":{}}'
  fi

  session=""
  local headers
  headers="$(curl -sS --max-time 30 -D- -o /dev/null \
    -H 'Content-Type: application/json' \
    -H 'Accept: application/json, text/event-stream' \
    "http://127.0.0.1:${port}/mcp" \
    -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"'"$PROTOCOL"'","capabilities":'"$capabilities"',"clientInfo":{"name":"smoke-test","version":"1"}}}')" ||
    fail "no answer from the server on port $port"

  session="$(printf '%s' "$headers" | tr -d '\r' | awk -F': ' 'tolower($1) == "mcp-session-id" { print $2 }')"
  [ -n "$session" ] || fail "the server answered without an MCP session id"

  mcp_post '{"jsonrpc":"2.0","method":"notifications/initialized"}' >/dev/null
}

fetch() {
  mcp_post '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"web_fetch","arguments":{"url":"'"$1"'"}}}'
}

# events pulls the JSON out of an SSE body, one object per line.
events() {
  sed -n 's/^data: //p' "$1"
}

# fetch_approving performs the exchange a human's client performs for an unknown
# host: the call is made, the server asks whether the host may be reached, the
# answer is sent back as a response to that request, and the original call then
# completes on the same stream.
fetch_approving() {
  local url="$1" decision="$2"
  local stream="${workdir}/stream.$RANDOM"

  curl -sS --max-time 60 --no-buffer \
    -H 'Content-Type: application/json' \
    -H 'Accept: application/json, text/event-stream' \
    -H "MCP-Protocol-Version: $PROTOCOL" \
    -H "Mcp-Session-Id: $session" \
    "http://127.0.0.1:${port}/mcp" \
    -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"web_fetch","arguments":{"url":"'"$url"'"}}}' \
    >"$stream" &
  local fetching=$!

  local i id=""
  for i in $(seq 1 150); do
    id="$(events "$stream" | jq -r 'select(.method == "elicitation/create") | .id' 2>/dev/null | head -1)"
    [ -n "$id" ] && break
    sleep 0.2
  done
  if [ -z "$id" ]; then
    kill "$fetching" 2>/dev/null || true
    fail "no approval was asked for before fetching $url: $(cat "$stream")"
  fi

  mcp_post '{"jsonrpc":"2.0","id":'"$id"',"result":{"action":"accept","content":{"decision":"'"$decision"'"}}}' >/dev/null
  wait "$fetching" || fail "the call was never answered after approving $url"
  cat "$stream"
}

# --- container ---------------------------------------------------------------

start() {
  local data="$1"
  shift
  container="askweb-smoke-$RANDOM"
  docker run -d --name "$container" -p "127.0.0.1:${port}:8080" \
    -v "${data}:/app/data" "$@" "$IMAGE" >/dev/null
  wait_for_server
}

wait_for_server() {
  local i
  for i in $(seq 1 50); do
    if curl -sS -o /dev/null --max-time 2 "http://127.0.0.1:${port}/mcp"; then
      return 0
    fi
    sleep 0.2
  done
  fail "the server never answered on port $port"
}

# --- one full pass -----------------------------------------------------------

run_pass() {
  local label="$1"
  shift

  echo "== $label"

  local data="${workdir}/${RANDOM}"
  mkdir -p "$data"
  # The operator's job, made literal: the mounted directory is given to whoever
  # the container runs as. The image deliberately does not do this itself.
  chmod 777 "$data"
  printf '[\n  "%s"\n]\n' "$KNOWN_HOST" >"$data/whitelist.json"
  chmod 666 "$data/whitelist.json"

  port="$(free_port)"
  start "$data" "$@"

  # 1. It answers MCP on the published port.
  mcp_connect can-prompt
  local tools
  tools="$(mcp_post '{"jsonrpc":"2.0","id":4,"method":"tools/list"}')"
  printf '%s' "$tools" | grep -q 'web_fetch' || fail "tools/list did not offer web_fetch: $tools"
  pass "answers MCP on the published port"

  # 2. It fetches a whitelisted https host, which needs the certificate store.
  local body
  body="$(fetch "$KNOWN_URL")"
  printf '%s' "$body" | grep -q 'x509' && fail "no certificate store in the image: $body"
  printf '%s' "$body" | grep -q "$KNOWN_MARKER" || fail "fetching $KNOWN_URL failed: $body"
  pass "fetches a whitelisted https host"

  # 3. An "always" approval reaches the mounted whitelist and outlives the
  #    container. The restart is literal, unlike the Go suite's version.
  body="$(fetch_approving "$APPROVED_URL" always)"
  printf '%s' "$body" | grep -q 'isError.*true' && fail "the approved fetch of $APPROVED_URL failed: $body"
  jq -e --arg h "$APPROVED_HOST" 'index($h)' "$data/whitelist.json" >/dev/null ||
    fail "an approved host never reached the mounted whitelist: $(cat "$data/whitelist.json")"
  pass "an approval is written to the mounted whitelist"

  docker restart "$container" >/dev/null
  wait_for_server

  # A client that cannot be asked anything: the only way this host is reachable
  # now is from the whitelist the restarted server loaded.
  mcp_connect
  body="$(fetch "$APPROVED_URL")"
  printf '%s' "$body" | grep -q 'elicitation/create' && fail "the approval did not survive the restart: $body"
  printf '%s' "$body" | grep -q 'was not approved' && fail "the approval did not survive the restart: $body"
  pass "the approval survives a container restart"

  # The control: still fail-closed for everything nobody approved.
  body="$(fetch "$REFUSED_URL")"
  printf '%s' "$body" | grep -q 'was not approved' ||
    fail "an unapproved host was not refused: $body"
  pass "an unapproved host is still refused"

  docker rm -f "$container" >/dev/null
  container=""
}

free_port() {
  python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
}

# --- the two passes ----------------------------------------------------------

docker image inspect "$IMAGE" >/dev/null 2>&1 || fail "no such image: $IMAGE"

run_pass "as the image's default user"
run_pass "as an overridden uid and gid ($OVERRIDE_UID:$OVERRIDE_GID)" \
  --user "${OVERRIDE_UID}:${OVERRIDE_GID}"

echo "smoke test passed"
