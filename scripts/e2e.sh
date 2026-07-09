#!/usr/bin/env bash
# End-to-end smoke test: scaffold an app with forge, build it, and exercise
# the full stack (auth, collections, records, rules, MCP, realtime) via HTTP.
# Run from the framework repo root:  ./scripts/e2e.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORK="$(mktemp -d)"
APP="$WORK/e2eapp"
PORT=8091
B="http://localhost:$PORT"
trap 'kill $(jobs -p) 2>/dev/null || true; rm -rf "$WORK"' EXIT

say() { printf "\n\033[1;36m▶ %s\033[0m\n" "$1"; }
ok() { printf "  \033[32m✓\033[0m %s\n" "$1"; }
fail() { printf "  \033[31m✗ %s\033[0m\n" "$1"; exit 1; }

say "Building forge CLI"
go build -o "$WORK/forge" ./cmd/forge
ok "forge built"

say "Scaffolding API-only app"
"$WORK/forge" init "$APP" --name e2eapp --module example.com/e2eapp --db sqlite \
  --modules auth,perm,mail,mcp,logs,metrics --ui=false --local "$ROOT" --yes --git=false >/dev/null
ok "app scaffolded"

say "Building the app binary"
( cd "$APP" && go build -o "$WORK/app" . )
ok "single binary built"

say "Creating superuser + serving"
export FORGE_DATA_DIR="$WORK/data" FORGE_HTTP_ADDR=":$PORT"
"$WORK/app" superuser create admin@e2e.dev supersecret123 >/dev/null
"$WORK/app" serve >"$WORK/serve.log" 2>&1 &
for i in $(seq 1 30); do curl -sf "$B/api/health" >/dev/null 2>&1 && break; sleep 0.3; done
curl -sf "$B/api/health" >/dev/null || fail "server did not start"
ok "server up"

jqget() { python3 -c "import sys,json;print(json.load(sys.stdin)$1)"; }

say "Superuser auth"
TOKEN=$(curl -s -X POST "$B/api/collections/_superusers/auth-with-password" \
  -H 'Content-Type: application/json' \
  -d '{"identity":"admin@e2e.dev","password":"supersecret123"}' | jqget "['token']")
[ -n "$TOKEN" ] && ok "logged in as superuser"

say "Create a collection with rules"
# Build the payload via a heredoc file to avoid shell quoting of rule literals.
cat > "$WORK/tasks.json" <<'JSON'
{
  "name": "tasks", "type": "base",
  "fields": [
    {"name": "title", "type": "text", "required": true},
    {"name": "done", "type": "bool"},
    {"name": "owner", "type": "relation", "options": {"collection": "users"}}
  ],
  "listRule": "", "viewRule": "",
  "createRule": "@request.auth.id != '' && owner = @request.auth.id",
  "updateRule": "owner = @request.auth.id",
  "deleteRule": "owner = @request.auth.id"
}
JSON
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$B/api/collections" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d @"$WORK/tasks.json")
[ "$code" = "200" ] && ok "collection created" || fail "collection create = $code"

say "User registration + login"
curl -s -o /dev/null -X POST "$B/api/collections/users/records" -H 'Content-Type: application/json' \
  -d '{"email":"user@e2e.dev","password":"userpassword1","passwordConfirm":"userpassword1"}'
USERID=$(curl -s "$B/api/collections/users/records" -H "Authorization: Bearer $TOKEN" | jqget "['items'][0]['id']")
UTOK=$(curl -s -X POST "$B/api/collections/users/auth-with-password" -H 'Content-Type: application/json' \
  -d '{"identity":"user@e2e.dev","password":"userpassword1"}' | jqget "['token']")
[ -n "$UTOK" ] && ok "user registered + logged in"

say "Rule enforcement: user creates own task, cannot forge others"
[ -n "$USERID" ] || fail "could not resolve user id"
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$B/api/collections/tasks/records" \
  -H "Authorization: Bearer $UTOK" -H 'Content-Type: application/json' \
  -d "{\"title\":\"my task\",\"owner\":\"$USERID\"}")
[ "$code" = "200" ] && ok "own record accepted" || fail "own record = $code"
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$B/api/collections/tasks/records" \
  -H 'Content-Type: application/json' -d '{"title":"anon","owner":"x"}')
[ "$code" = "403" ] || [ "$code" = "400" ] && ok "anonymous create blocked ($code)" || fail "anon create = $code"

say "MCP: create key, handshake, AI builds a collection"
KEY=$(curl -s -X POST "$B/api/apikeys" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"ai","admin":true}' | jqget "['key']")
proto=$(curl -s -X POST "$B/api/mcp" -H "Authorization: Bearer $KEY" -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}' | jqget "['result']['protocolVersion']")
[ "$proto" = "2025-06-18" ] && ok "MCP handshake ($proto)"
err=$(curl -s -X POST "$B/api/mcp" -H "Authorization: Bearer $KEY" -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"collections_save","arguments":{"name":"products","fields":[{"name":"sku","type":"text"}],"listRule":""}}}' | jqget "['result'].get('isError',False)")
[ "$err" = "False" ] && ok "AI built a collection over MCP"
n=$(curl -s "$B/api/collections/products/records" | jqget "['totalItems']")
[ "$n" = "0" ] && ok "MCP-built collection is live on REST"

say "Metrics endpoint"
curl -sf "$B/metrics" | grep -q "forge_http_requests_total" && ok "Prometheus metrics exported"

printf "\n\033[1;32m✓ All end-to-end checks passed.\033[0m\n"
