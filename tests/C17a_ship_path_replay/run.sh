#!/usr/bin/env bash
# C17a — the deterministic project-add replay (WO-16).
# Walks a brand-new component through the ENTIRE ship path using ONLY
# what `fleet next` prints, exactly as an agent would: onboard → promote
# dev → approve dev → promote stage → approve prod (owner-via-agent per
# the actor policy) → promote prod → ops build → ops deploy → NEXT none.
# The binary is the process; the model is irrelevant. Hermetic: no git
# (P1 skips), fake kubectl, fake curl, minimal gates, scratch secrets.

source "$FLEET_ROOT/scripts/lib.sh"

scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
export FLEET_SECRETS_HOME="$scratch/secrets-home"
mkdir -p "$FLEET_SECRETS_HOME"

F="$(bash "$FLEET_ROOT/ci/build-fleet.sh" "$scratch/fleet-bin" >/dev/null; echo "$scratch/fleet-bin")"
[[ -x "$F" ]] || { report_fail "binary builds" "ci/build-fleet.sh failed"; finalize; }

mkdir -p "$scratch/repo"
tar -C "$FLEET_ROOT" --exclude=.git --exclude=.vm --exclude=dist -cf - . | tar -C "$scratch/repo" -xf -
cd "$scratch/repo" || exit 1

# empty component registry + minimal gates + single drill site (labfix)
python3 - "$scratch" <<'EOF'
import sys, pathlib
scratch = sys.argv[1]
p = pathlib.Path(scratch, "repo/ops/PROJECTS.yaml")
t = p.read_text()
head = t.split("components:")[0]
p.write_text(head + "components: []\n")
g = pathlib.Path(scratch, "repo/lifecycle/gates.yaml")
g.write_text("""# minimal gates for the C17a replay
gates_version: 1
gates:
  - from: built
    to: dev
    requires_units: []
    needs_approvals: []
  - from: dev
    to: stage
    requires_units: []
    needs_approvals:
      - dev
  - from: stage
    to: prod
    requires_units: []
    needs_approvals:
      - dev
      - prod
""")
s = pathlib.Path(scratch, "repo/ops/SITES.yaml")
t = s.read_text()
s.write_text(f"""sites_version: 1
sites:
  - name: drill
    engine: sos-lab
    lab_root: {scratch}/lab
    namespace: sos-lab
    access: kubeconfig:{scratch}/drill.kubeconfig
    description: C17a replay site
""")
EOF

mkdir -p "$scratch/lab"
cp -r "$FLEET_ROOT/internal/fleet/testdata/labfix/." "$scratch/lab/"
# alpha must be BUILDABLE for the replay (repo, not a prebuilt image)
sed -i -e "s|^    image: reg.local:5000/alpha:v1$|    repo: fleet/alpha-src|" -e "/    host: alpha.example.test/d" "$scratch/lab/config/registry.yaml"
: > "$scratch/drill.kubeconfig"
mkdir -p "$FLEET_SECRETS_HOME/drill"
printf 'ALPHA_KEY=dummy\n' > "$FLEET_SECRETS_HOME/drill/alpha.env"
printf 'DASHBOARD_SLUG=replaydash\n' > "$FLEET_SECRETS_HOME/drill/dashboard.env"
mkdir -p "$scratch/repo/alpha-src" "$scratch/fleet/alpha-src"
printf 'FROM busybox:1.36\n' > "$scratch/repo/alpha-src/Dockerfile"
printf 'FROM busybox:1.36\n' > "$scratch/fleet/alpha-src/Dockerfile"

# fake kubectl (same discipline as C13b: rule-7 env, canned answers)
mkdir -p "$scratch/bin" "$scratch/records"
cat > "$scratch/bin/kubectl" <<'FAKE'
#!/usr/bin/env bash
rest=$(env | sed '/^KUBECONFIG=/d; /^HOME=/d; /^_=/d; /^PWD=/d; /^SHLVL=/d')
if [ -n "$rest" ]; then echo "RULE7 VIOLATION" >&2; exit 99; fi
rec="$(dirname "$0")/../records"
n=$(( $(ls "$rec" 2>/dev/null | grep -c '\.args$') + 1 ))
printf '%s\n' "$*" > "$rec/$n.args"
if [ "$1" = "apply" ] && [ "$2" = "-f" ] && [ "$3" = "-" ]; then cat > "$rec/$n.stdin"; fi
case "$*" in
  *"create secret generic"*) printf 'apiVersion: v1\nkind: Secret\nmetadata:\n  name: alpha-env\n' ;;
  "apply -f -") echo "configured" ;;
  apply\ -f\ *) : ;;
  *"rollout status"*|*"rollout undo"*|*"rollout restart"*) : ;;
  *"delete deployment"*|*"delete service"*|*"delete pvc"*|*"delete job"*) : ;;
  *"get rs"*) echo '{"items":[{"metadata":{"annotations":{"deployment.kubernetes.io/revision":"2"}},"spec":{"template":{"spec":{"containers":[{"image":"reg.local:5000/alpha:t1"}]}}}}]}' ;;
  *"get pods"*) echo '{"items":[{"metadata":{"name":"build-pod-0"}}]}' ;;
  *"logs -f"*) echo "build-log-line" ;;
  *"wait --for=condition=complete"*) : ;;
  *) echo "unexpected: $*" >> "$rec/unexpected"; exit 97 ;;
esac
FAKE
cat > "$scratch/records/rs.json" <<'EOF'
{"items":[{"metadata":{"annotations":{"deployment.kubernetes.io/revision":"1"}},"spec":{"template":{"spec":{"containers":[{"image":"reg.local:5000/alpha:t0"}]}}}}]}
EOF
cat > "$scratch/bin/curl" <<'CURL'
#!/usr/bin/env bash
echo "200"
CURL
chmod +x "$scratch/bin/kubectl" "$scratch/bin/curl"
export PATH="$scratch/bin:$PATH"

fl() { FLEET_ROOT="$scratch/repo" "$F" "$@" 2>&1; }
next_action() { fl next | grep '^NEXT action=' | head -1 | sed 's/^NEXT action=//'; }

# 0. cold start: no components -> none (nothing owed yet)
assert_contains "cold start: none" "NEXT action=none" "$(fl next)"

# 1. onboard (state lands at built)
o="$(fl onboard alpha --kind=service --path=alpha-src --entrypoint=Dockerfile --description=ship-path-replay)"
assert_contains "onboard ok" "ONBOARDED component=alpha" "$o"

# 2. next -> promote dev
assert_eq "next: promote dev" "./scripts/fleet promote alpha dev" "$(next_action)"
assert_contains "promote dev ok" "PROMOTED component=alpha" "$(fl promote alpha dev)"

# 3. next -> approve dev (middle hop, agent actor)
assert_eq "next: approve dev" "./scripts/fleet approve alpha dev" "$(next_action)"
assert_contains "approve dev ok" "APPROVED component=alpha stage=dev" "$(fl approve alpha dev agent)"

# 4. next -> promote stage
assert_eq "next: promote stage" "./scripts/fleet promote alpha stage" "$(next_action)"
assert_contains "promote stage ok" "PROMOTED component=alpha" "$(fl promote alpha stage)"

# 5. next -> approve prod (human-gated: agent refusal is the LAW)
o="$(FLEET_ACTOR=agent FLEET_ROOT="$scratch/repo" "$F" approve alpha prod agent 2>&1)"
assert_contains "prod approval refused for agent" "may not approve stage" "$o"
assert_eq "next: approve prod" "./scripts/fleet approve alpha prod" "$(next_action)"
o="$(FLEET_ACTOR=owner-via-agent FLEET_ROOT="$scratch/repo" "$F" approve alpha prod owner-via-agent 2>&1)"
assert_contains "prod approval via owner-via-agent" "APPROVED component=alpha stage=prod" "$o"

# 6. next -> promote prod
assert_eq "next: promote prod" "./scripts/fleet promote alpha prod" "$(next_action)"
assert_contains "promote prod ok" "PROMOTED component=alpha" "$(fl promote alpha prod)"

# 7. next -> ops build (the site owes the kaniko build)
assert_eq "next: ops build" "./scripts/fleet ops build --site drill alpha" "$(next_action)"
assert_contains "build ok" "BUILT alpha" "$(fl ops build --site drill alpha)"

# 8. next -> ops deploy
assert_eq "next: ops deploy" "./scripts/fleet ops deploy --site drill alpha" "$(next_action)"
assert_contains "deploy ok" "DEPLOYED alpha" "$(fl ops deploy --site drill alpha)"

# 9. shipped: next -> none
assert_contains "shipped: next none" "NEXT action=none" "$(fl next)"

[[ -f "$scratch/records/unexpected" ]] \
  && report_fail "no unexpected kubectl calls" "$(cat "$scratch/records/unexpected")" \
  || report_pass "no unexpected kubectl calls"

finalize
