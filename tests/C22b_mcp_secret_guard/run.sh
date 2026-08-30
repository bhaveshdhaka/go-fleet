#!/usr/bin/env bash
# C22b — `fleet mcp` secret-leak guard (WO-22 phase 1, WO-14 house rule).
# Hermetic: fake kubectl serves the cluster answers; the site's secrets
# home is seeded with a UNIQUE canary value. The negative control proves
# the canary is live on disk; the guard then drives every read tool and
# resource through the stdio server and asserts the canary value (and
# every value line of the seeded env files) NEVER appears in any server
# output. Key NAMES may appear; VALUES must not.

source "$FLEET_ROOT/scripts/lib.sh"

scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
F="$FLEET_ROOT/dist/fleet"
[[ -x "$F" ]] || F="$(bash "$FLEET_ROOT/ci/build-fleet.sh" "$scratch/fleet-bin" >/dev/null; echo "$scratch/fleet-bin")"

CANARY="MCPGUARD-$(head -c 8 /dev/urandom | od -An -tx1 | tr -d ' \n')-0xDEAD"
# od is fine; keep the canary free of regex-active chars for greps
CANARY="MCPGUARD$(date +%s)x"

# --- jsonq: parse gate for recv_for + canary greps
jsonq_build "$scratch/jsonq" || { report_fail "jsonq builds" "go build failed"; finalize; }
J="$scratch/jsonq"

# --- fake kubectl (C12c pattern): canned cluster, refuses everything else
mkdir -p "$scratch/bin"
cat > "$scratch/bin/kubectl" <<'FAKE'
#!/usr/bin/env bash
case "$*" in
  "get nodes -o wide") printf 'NAME   STATUS\nhk-03-dev   Ready\n' ;;
  "get nodes -o name") echo "node/hk-03-dev" ;;
  "get nodes -o json") echo '{"items":[{"status":{"conditions":[{"type":"Ready","status":"True"}]}}]}' ;;
  *"get pods -o wide") printf 'NAME   READY   STATUS\ndemo-1   1/1   Running\n' ;;
  *"jsonpath={.items[*].status.phase}"*) echo "Running" ;;
  *"get deployment"*"jsonpath"*) echo "" ;;
  *) echo "unexpected: $*" >&2; exit 97 ;;
esac
FAKE
chmod +x "$scratch/bin/kubectl"

# --- fixture repo: real registry/lab shape (tar copy), secrets OUTSIDE
repo="$scratch/repo"
mkdir -p "$repo"
tar -C "$FLEET_ROOT" --exclude=.git --exclude=.vm --exclude=dist --exclude=vendor -cf - . | tar -C "$repo" -xf -
lab="$scratch/lab-fixture"
mkdir -p "$lab/config" "$lab/state" "$lab/secrets"
cat > "$lab/config/registry.yaml" <<'EOF'
cloudflare:
  account_id: acct123
  tunnel_id: tun123
  tunnel_name: lab
domains:
  example.test:
    zone_id: zone123
services:
  alpha:
    port: 8080
    namespace: fleet
    host: alpha.example.test
    enabled: true
EOF
printf '{"alpha": {"tag": "t1", "image": "reg/alpha:t1", "git_sha": "abcdef1234567890"}}\n' > "$lab/state/deployed.json"
: > "$scratch/kubeconfig"

sed -i -e "s|^    lab_root: .*|    lab_root: $lab|" \
       -e "s|^    access: .*|    access: kubeconfig:$scratch/kubeconfig|" \
       "$repo/ops/SITES.yaml"

# --- secrets home: the ONLY place values live (WO-14). One canary per file.
secret_home="$scratch/secrets-home/hk-03-dev"
mkdir -p "$secret_home"
printf 'CF_API_TOKEN=%s\n' "$CANARY" > "$secret_home/cloudflare.env"
printf 'TRIO_DB_PASS=%s\nBACKUP_KEY=%s\n' "$CANARY" "$CANARY" > "$secret_home/trio.env"

# --- negative control: the canary IS live on disk
grep -q "$CANARY" "$secret_home/cloudflare.env" \
  && report_pass "negative control: canary seeded in secrets home" \
  || report_fail "negative control: canary seeded in secrets home" "canary missing on disk"

# --- drive the server: every read tool + every resource
fifo="$scratch/in"
OUT="$scratch/out"
mkfifo "$fifo"
PATH="$scratch/bin:$PATH" FLEET_ROOT="$repo" FLEET_SECRETS_HOME="$scratch/secrets-home" \
  "$F" mcp < "$fifo" > "$OUT" 2> "$scratch/err" &
MCP_PID=$!
exec 9>"$fifo"

send() { printf '%s\n' "$1" >&9; }
recv_for() { # poll by id; response is final once jsonq parses it whole
  local want="$1" deadline=$((SECONDS + 20)) cand
  while (( SECONDS < deadline )); do
    cand="$(grep -a -o "[{]\"jsonrpc\":\"2.0\",\"id\":$want,.*" "$OUT" 2>/dev/null | head -1)"
    if [[ -n "$cand" ]]; then
      printf '%s' "$cand" > "$scratch/cand.json"
      if "$J" "$scratch/cand.json" valid > /dev/null 2>&1; then
        printf '%s' "$cand"
        return 0
      fi
    fi
    sleep 0.2
  done
  return 1
}

send '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"c22b","version":"0"}}}'
recv_for 1 > "$scratch/resp-1.json" || report_fail "initialize" "no response"
send '{"jsonrpc":"2.0","method":"notifications/initialized"}'

id=10
for tool in fleet_status fleet_next fleet_check fleet_sites fleet_wo_list ops_status; do
  id=$((id + 1))
  send "{\"jsonrpc\":\"2.0\",\"id\":$id,\"method\":\"tools/call\",\"params\":{\"name\":\"$tool\",\"arguments\":{}}}"
  recv_for "$id" > "$scratch/resp-$id.json" || report_fail "tool $tool called" "no response"
done

id=$((id + 1))
send "{\"jsonrpc\":\"2.0\",\"id\":$id,\"method\":\"tools/call\",\"params\":{\"name\":\"fleet_wo_show\",\"arguments\":{\"id\":\"WO-1\"}}}"
recv_for "$id" > "$scratch/resp-$id.json" || report_fail "fleet_wo_show called" "no response"

id=$((id + 1))
send "{\"jsonrpc\":\"2.0\",\"id\":$id,\"method\":\"tools/call\",\"params\":{\"name\":\"fleet_wo_show\",\"arguments\":{\"id\":\"WO-22\"}}}"
recv_for "$id" > "$scratch/resp-$id.json" || report_fail "fleet_wo_show WO-22 called" "no response"

for uri in fleet://lifecycle/journal fleet://registry/projects fleet://registry/state fleet://registry/sites fleet://lifecycle/gates; do
  id=$((id + 1))
  send "{\"jsonrpc\":\"2.0\",\"id\":$id,\"method\":\"resources/read\",\"params\":{\"uri\":\"$uri\"}}"
  recv_for "$id" > "$scratch/resp-$id.json" || report_fail "resource $uri read" "no response"
done

exec 9>&-
wait "$MCP_PID" 2>/dev/null

# --- THE GUARD: canary value must not appear anywhere in server output
cat "$scratch/out" "$scratch/err" "$scratch"/resp-*.json > "$scratch/all-output.txt" 2>/dev/null
if grep -q "$CANARY" "$scratch/all-output.txt"; then
  report_fail "canary value absent from ALL server output" \
    "SECRET LEAK: $(grep -o ".\{0,40\}$CANARY.\{0,20\}" "$scratch/all-output.txt" | head -2)"
else
  report_pass "canary value absent from ALL server output"
fi

# every value line of the seeded env files, individually
leaked=""
while IFS= read -r line; do
  [[ -z "$line" || "$line" == \#* ]] && continue
  val="${line#*=}"
  [[ -z "$val" ]] && continue
  if grep -qF "$val" "$scratch/all-output.txt"; then
    leaked="${leaked}${val} "
  fi
done < <(cat "$secret_home"/*.env)
if [[ -n "$leaked" ]]; then
  report_fail "no seeded secret value appears in server output" "leaked: $leaked"
else
  report_pass "no seeded secret value appears in server output"
fi

# sanity: the session actually ran (ops_status answered through fake kubectl)
if grep -q '"id":11' "$scratch/out"; then
  report_pass "session drove the full read surface"
else
  report_fail "session drove the full read surface" "id=11 response missing"
fi

finalize
