package fleet

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const regFixture = `# registry fixture
registry_version: 1

components:

  - name: fleetctl
    kind: cli
    path: apps/fleetctl
    language: go
    pipeline: ci/pipelines/fleetctl.yaml
    manifests: infra/k8s
    enabled: true

  - name: fleethub
    kind: service
    port: 8099
    bind_default: "127.0.0.1:8099"
    path: apps/fleethub
    enabled: true
`

const stateFixture = `# state fixture
state_version: 1

components:

  - name: fleetctl
    stage: prod
    last_promoted_at: "2026-08-27T14:28:43Z"
    note: fresh checkout baseline

  - name: fleethub
    stage: built
    last_promoted_at: ""
    note: registered ahead of C6 delivery
`

const gatesFixture = `# gates fixture
gates_version: 1

gates:

  - from: built
    to: dev
    requires_units:
      - C3c_build_reproducible
    needs_approvals: []

  - from: dev
    to: stage
    requires_units:
      - C3c_build_reproducible
      - C4a_deploy_static
    needs_approvals:
      - dev

  - from: stage
    to: prod
    requires_units:
      - C3c_build_reproducible
      - C4a_deploy_static
      - C6c_webapp_contract
    needs_approvals:
      - dev
      - prod
`

func lines(s string) []string { return strings.Split(s, "\n") }

func TestRegistryNamesAndFields(t *testing.T) {
	ls := lines(regFixture)
	got := registryNames(ls)
	want := []string{"fleetctl", "fleethub"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("registryNames = %v, want %v", got, want)
	}
	cases := map[[2]string]string{
		{"fleetctl", "kind"}:         "cli",
		{"fleetctl", "path"}:         "apps/fleetctl",
		{"fleetctl", "enabled"}:      "true",
		{"fleethub", "kind"}:         "service",
		{"fleethub", "port"}:         "8099",
		{"fleethub", "bind_default"}: "127.0.0.1:8099",
		{"fleethub", "path"}:         "apps/fleethub",
		{"ghost", "kind"}:            "",
	}
	for k, want := range cases {
		if got := fieldFor(ls, k[0], k[1]); got != want {
			t.Errorf("fieldFor(%s,%s) = %q, want %q", k[0], k[1], got, want)
		}
	}
}

func TestStateStageExtraction(t *testing.T) {
	ls := lines(stateFixture)
	if got := stateStage(ls, "fleetctl"); got != "prod" {
		t.Errorf("stateStage fleetctl = %q", got)
	}
	if got := stateStage(ls, "fleethub"); got != "built" {
		t.Errorf("stateStage fleethub = %q", got)
	}
	if got := promoteCurrentStage(ls, "fleethub"); got != "built" {
		t.Errorf("promoteCurrentStage fleethub = %q", got)
	}
	if got := promoteCurrentStage(ls, "ghost"); got != "" {
		t.Errorf("promoteCurrentStage ghost = %q", got)
	}
}

func TestGateUnitsAll(t *testing.T) {
	got := GateUnitsAll(lines(gatesFixture))
	want := []string{
		"C3c_build_reproducible",
		"C3c_build_reproducible", "C4a_deploy_static",
		"C3c_build_reproducible", "C4a_deploy_static", "C6c_webapp_contract",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GateUnitsAll = %v, want %v", got, want)
	}
}

func TestGateEdgeItems(t *testing.T) {
	ls := lines(gatesFixture)
	cases := []struct {
		from, to string
		want     []GateEdgeItem
	}{
		{"built", "dev", []GateEdgeItem{{"U", "C3c_build_reproducible"}}},
		{"dev", "stage", []GateEdgeItem{
			{"U", "C3c_build_reproducible"}, {"U", "C4a_deploy_static"}, {"A", "dev"},
		}},
		{"stage", "prod", []GateEdgeItem{
			{"U", "C3c_build_reproducible"}, {"U", "C4a_deploy_static"},
			{"U", "C6c_webapp_contract"}, {"A", "dev"}, {"A", "prod"},
		}},
		{"built", "prod", nil},
		{"dev", "dev", nil},
	}
	for _, c := range cases {
		got := GateEdgeItems(ls, c.from, c.to)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("GateEdgeItems(%s,%s) = %v, want %v", c.from, c.to, got, c.want)
		}
	}
}

func TestRewriteStateStageByteExact(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "deployments.yaml")
	if err := os.WriteFile(p, []byte(stateFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RewriteStateStage(p, "fleethub", "dev", "2026-08-27T15:00:00Z"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(stateFixture,
		"  - name: fleethub\n    stage: built\n    last_promoted_at: \"\"",
		"  - name: fleethub\n    stage: dev\n    last_promoted_at: \"2026-08-27T15:00:00Z\"", 1)
	if string(got) != want {
		t.Fatalf("rewrite mismatch:\n got: %q\nwant: %q", string(got), want)
	}
	if !strings.Contains(string(got), "2026-08-27T14:28:43Z") {
		t.Error("other component's timestamp must be untouched")
	}
}

func TestApprovalWriteAndRead(t *testing.T) {
	dir := t.TempDir()
	p := Paths{Approvals: dir}
	ap := ApprovalPath(p, "dev", "fleetctl")
	if HasApproval(ap) {
		t.Fatal("approval must not exist yet")
	}
	if err := WriteApproval(ap, "agent", "2026-08-27T15:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if !HasApproval(ap) {
		t.Fatal("approval file should exist and be non-empty")
	}
	b, _ := os.ReadFile(ap)
	if string(b) != "approved_by=agent\nts=2026-08-27T15:00:00Z\n" {
		t.Fatalf("approval content = %q", string(b))
	}
}

func TestJournalAppend(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "events.log")
	if err := AppendJournal(p, "ts=X event=approved component=c stage=dev actor=a"); err != nil {
		t.Fatal(err)
	}
	if err := AppendJournal(p, "ts=Y event=promoted component=c from=built to=dev actor=a"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	want := "ts=X event=approved component=c stage=dev actor=a\n" +
		"ts=Y event=promoted component=c from=built to=dev actor=a\n"
	if string(b) != want {
		t.Fatalf("journal = %q, want %q", string(b), want)
	}
}

func TestBashTrJoin(t *testing.T) {
	if got := bashTrJoin(nil); got != "" {
		t.Errorf("empty = %q", got)
	}
	if got := bashTrJoin([]string{"a", "b"}); got != "a b " {
		t.Errorf("tr join = %q, want %q", got, "a b ")
	}
}

func TestFindRootWalkUp(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "ops")
	os.MkdirAll(sub, 0o755)
	os.WriteFile(filepath.Join(sub, "PROJECTS.yaml"), []byte("x"), 0o644)
	deep := filepath.Join(dir, "a", "b", "c")
	os.MkdirAll(deep, 0o755)
	if r, ok := walkUp(deep); !ok || r != dir {
		t.Fatalf("walkUp = %q %v, want %q", r, ok, dir)
	}
	if _, ok := walkUp(t.TempDir()); ok {
		t.Error("walkUp must fail with no ops/PROJECTS.yaml")
	}
}

func TestFleetTS(t *testing.T) {
	ref, err := time.Parse(time.RFC3339, "2026-08-27T14:28:43Z")
	if err != nil {
		t.Fatal(err)
	}
	if got := FleetTS(ref); got != "2026-08-27T14:28:43Z" {
		t.Errorf("FleetTS roundtrip = %q", got)
	}
}
