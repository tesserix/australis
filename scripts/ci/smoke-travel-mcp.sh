#!/usr/bin/env bash
set -euo pipefail
: "${MCP_IMAGE:?}" "${MCP_PREFIX:?}"
case "$MCP_PREFIX" in ROAMIE|BOOKING|KLOOK) ;; *) exit 1 ;; esac
name="travel-mcp-smoke-$$"
cleanup() {
  result=$?
  if ((result != 0)); then docker logs --tail 60 "$name" || true; fi
  docker rm -f "$name" >/dev/null 2>&1 || true
}
trap cleanup EXIT
args=()
for pair in "KEY=$(openssl rand -hex 32)" "ISSUER=https://identity.example" "AUDIENCE=smoke" "JWKS_URL=https://identity.example/keys" "JWKS_HOSTS=identity.example" "GATEWAY_CIDRS=127.0.0.1/32" "ALLOWED_HOSTS=127.0.0.1" "ALLOWED_ORIGINS=https://smoke.example" "HOST=0.0.0.0" "LISTEN_HOST=0.0.0.0"; do
  args+=(-e "${MCP_PREFIX}_MCP_${pair}")
done
args+=(-e ROAMIE_MCP_API_ORIGIN=http://127.0.0.1:9000 -e ROAMIE_MCP_API_TOKEN=isolated-smoke-only)
docker run -d --name "$name" -p 127.0.0.1:8080:8080 "${args[@]}" "$MCP_IMAGE" >/dev/null
for ((attempt=0; attempt<30; attempt++)); do
  if curl -fsS http://127.0.0.1:8080/readyz >/dev/null; then break; fi
  sleep 1
done
curl -fsS http://127.0.0.1:8080/readyz >/dev/null
status=$(curl -sS -o /dev/null -w '%{http_code}' -X POST -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' --data '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"smoke","version":"1"}}}' http://127.0.0.1:8080/mcp)
test "$status" = 401
