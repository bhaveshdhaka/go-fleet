package fleet

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// WO-8 parity tests: the Go renderers/writers must produce the same
// object graphs and byte formats as the authoritative python engine
// (labctl 2.0.0) for the same inputs.
//
// The oracle is FROZEN EVIDENCE, not a live dependency: goldens under
// testdata/golden were generated once by the real labctl from the
// testdata/labfix registry (sos-lab is authoritative until WO-8 cutover
// and frozen thereafter). The tests below are pure Go — no python, no
// sibling repo, no network, no cluster. The live python-vs-fleet run was
// performed once as a journaled drill and is repeated at the piece-3
// dual-run.

const labFix = "testdata/labfix"

// copyDir is a tiny recursive copy for test fixtures.
func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	if err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, raw, 0o644)
	}); err != nil {
		t.Fatalf("copy fixture: %v", err)
	}
}

func loadGolden(t *testing.T, rel string) any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "golden", rel))
	if err != nil {
		t.Fatalf("golden %s unreadable: %v", rel, err)
	}
	v, err := decodeJSONNumber(raw)
	if err != nil {
		t.Fatalf("golden %s corrupt: %v", rel, err)
	}
	return v
}

func TestLabRenderParity(t *testing.T) {
	scratch := t.TempDir()
	copyDir(t, labFix, scratch)

	site := Site{Name: "fix", Engine: "sos-lab", LabRoot: scratch, Namespace: "sos-lab", Access: "in-cluster"}
	lv, err := LoadLabView(site, scratch)
	if err != nil {
		t.Fatalf("LoadLabView: %v", err)
	}

	wantSvcs := loadGolden(t, "services.json").(map[string]any)
	for _, name := range lv.LabServiceNames() {
		svc := lv.LabServices()[name]
		img, err := labServiceImage(lv, name, svc)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got := renderLabService(lv, scratch, name, svc, img)
		assertParity(t, fmt.Sprintf("render_service/%s", name), got, wantSvcs[name].([]any)...)
	}

	assertParity(t, "render_kaniko_job/single",
		[]any{renderLabKanikoJob("sos-lab", "build-x-1", "/workspace/x", "reg.local:5000/x:t1", nil, "Dockerfile")},
		loadGolden(t, "kaniko_single.json"))
	assertParity(t, "render_kaniko_job/overlay",
		[]any{renderLabKanikoJob("sos-lab", "build-x-ovr-1", "/workspace/x", "reg.local:5000/x:t1",
			[]string{"--build-arg=BASE_IMAGE=reg.local:5000/x-base:t1", "--build-arg=KUBE_VERSION=v1.36.3", "--insecure-pull"},
			"Dockerfile")},
		loadGolden(t, "kaniko_overlay.json"))
	assertParity(t, "tunnel_ingress_from_registry",
		[]any{renderLabTunnelIngress(lv)}, loadGolden(t, "tunnel_ingress.json"))

	docs, err := renderLabMonitorDocs(lv, scratch, "fixdeadbeef")
	if err != nil {
		t.Fatalf("renderLabMonitorDocs: %v", err)
	}
	assertParity(t, "monitor_docs", docs, loadGolden(t, "monitor.json").([]any)...)
}

// assertParity compares rendered object graphs via canonical JSON bytes.
func assertParity(t *testing.T, name string, got []any, want ...any) {
	t.Helper()
	gotB, err := jsonMarshalIndent(got)
	if err != nil {
		t.Fatalf("%s: marshal got: %v", name, err)
	}
	wantB, err := jsonMarshalIndent(want)
	if err != nil {
		t.Fatalf("%s: marshal want: %v", name, err)
	}
	if string(gotB) != string(wantB) {
		gotS, wantS := string(gotB), string(wantB)
		i := 0
		for i < len(gotS) && i < len(wantS) && gotS[i] == wantS[i] {
			i++
		}
		lo := i - 120
		if lo < 0 {
			lo = 0
		}
		t.Fatalf("%s: object graphs differ at byte %d\nGOT : %s\nWANT: %s",
			name, i, gotS[lo:min(i+240, len(gotS))], wantS[lo:min(i+240, len(wantS))])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestLabStateParity asserts the state-file byte format against the
// frozen json.dump(indent=2, sort_keys=True) references for the three
// recorders.
func TestLabStateParity(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 34, 56, 0, time.UTC)
	t.Setenv("SOS_LAB_NODE", "hk-03-dev")

	cases := []struct {
		name string
		file string // file the recorder writes
		run  func(labRoot string) error
	}{
		{
			name: "deploy-with-sha",
			file: "deployed.json",
			run: func(labRoot string) error {
				_, err := recordLabDeploy(labRoot, "alpha", "t9", "reg/x:t9", "abcd1234abcd1234", now)
				return err
			},
		},
		{
			name: "deploy-no-sha",
			file: "deployed.json",
			run: func(labRoot string) error {
				_, err := recordLabDeploy(labRoot, "alpha", "t9", "reg/x:t9", "", now)
				return err
			},
		},
		{
			name: "build-null-sha",
			file: "builds.json",
			run: func(labRoot string) error {
				return recordLabBuild(labRoot, "beta", "t8", "", now)
			},
		},
		{
			name: "rollback",
			file: "deployed.json",
			run: func(labRoot string) error {
				_, err := recordLabRollback(labRoot, "alpha", "t7", "reg/x:t7", now)
				return err
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			goScratch := t.TempDir()
			st := filepath.Join(goScratch, "state")
			if err := os.MkdirAll(st, 0o755); err != nil {
				t.Fatal(err)
			}
			for _, f := range []string{"deployed.json", "builds.json"} {
				if err := os.WriteFile(filepath.Join(st, f), []byte("{}\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := tc.run(goScratch); err != nil {
				t.Fatalf("record failed: %v", err)
			}
			got, err := os.ReadFile(filepath.Join(st, tc.file))
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join("testdata", "golden", "state", tc.name, tc.file))
			if err != nil {
				t.Fatalf("state golden unreadable: %v", err)
			}
			if string(got) != string(want) {
				t.Fatalf("state bytes differ\nGOT : %q\nWANT: %q", got, want)
			}
		})
	}
}

// TestGatusEmitter covers the bounded yaml emitter shapes against the
// exact strings pyyaml produces (measured from the real engine).
func TestGatusEmitter(t *testing.T) {
	got := pyGatusDump([]any{
		map[string]any{
			"name":       "alpha",
			"url":        "https://alpha.example.test",
			"interval":   "60s",
			"conditions": []any{"[STATUS] == 200"},
		},
	})
	want := "endpoints:\n- name: alpha\n  url: https://alpha.example.test\n  interval: 60s\n  conditions:\n  - '[STATUS] == 200'\n"
	if got != want {
		t.Fatalf("gatus dump mismatch\nGOT : %q\nWANT: %q", got, want)
	}
	if got := pyGatusDump(nil); got != "endpoints: []\n" {
		t.Fatalf("empty gatus dump: %q", got)
	}
}

// TestRegistryEdits covers the surgical registry editor: flip + remove,
// each validated by re-parse, on a scratch copy of the fixture.
func TestRegistryEdits(t *testing.T) {
	scratch := t.TempDir()
	copyDir(t, labFix, scratch)

	if err := labSetServiceEnabled(scratch, "beta", true); err != nil {
		t.Fatalf("flip beta: %v", err)
	}
	site := Site{Name: "fix", Engine: "sos-lab", LabRoot: scratch, Namespace: "sos-lab", Access: "in-cluster"}
	lv, err := LoadLabView(site, scratch)
	if err != nil {
		t.Fatalf("re-parse after flip: %v", err)
	}
	if !asBool(lv.LabServices()["beta"]["enabled"]) {
		t.Fatalf("beta.enabled not true after flip")
	}
	// beta is repo-based: enabling with a host would route it; keep the
	// fixture semantics intact by disabling again.
	if err := labSetServiceEnabled(scratch, "beta", false); err != nil {
		t.Fatalf("flip back: %v", err)
	}
	if err := labSetServiceEnabled(scratch, "nonexistent", true); err == nil ||
		!strings.Contains(err.Error(), "not registered") {
		t.Fatalf("unknown service must be refused as 'not registered', got: %v", err)
	}

	// line-level check: flip rewrote exactly one line
	raw, _ := os.ReadFile(filepath.Join(scratch, "config", "registry.yaml"))
	if !strings.Contains(string(raw), "    enabled: false\n") {
		t.Fatalf("beta.enabled line not false after flip-back")
	}

	if err := labRemoveService(scratch, "gamma"); err != nil {
		t.Fatalf("remove gamma: %v", err)
	}
	lv2, err := LoadLabView(site, scratch)
	if err != nil {
		t.Fatalf("re-parse after remove: %v", err)
	}
	if _, ok := lv2.LabServices()["gamma"]; ok {
		t.Fatalf("gamma still present after remove")
	}
	if _, ok := lv2.LabServices()["alpha"]; !ok {
		t.Fatalf("alpha must survive gamma removal")
	}
	if err := labRemoveService(scratch, "gamma"); err == nil ||
		!strings.Contains(err.Error(), "not registered") {
		t.Fatalf("absent service must be refused as 'not registered', got: %v", err)
	}
}

// TestLabEnvOrder asserts the file-order recovery for env keys.
func TestLabEnvOrder(t *testing.T) {
	scratch := t.TempDir()
	copyDir(t, labFix, scratch)
	site := Site{Name: "fix", Engine: "sos-lab", LabRoot: scratch, Namespace: "sos-lab", Access: "in-cluster"}
	lv, err := LoadLabView(site, scratch)
	if err != nil {
		t.Fatal(err)
	}
	svc := lv.LabServices()["alpha"]
	got := labEnvOrder(scratch, "alpha", svc)
	want := []string{"BASE_URL", "DATABASE_URI"} // file order, NOT sorted
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("env order: got %v want %v", got, want)
	}
	// service without env: nil
	if got := labEnvOrder(scratch, "beta", lv.LabServices()["beta"]); got != nil {
		t.Fatalf("beta env order should be nil, got %v", got)
	}
}

// TestCloudflareWrites drives EnsureCname/PutTunnelConfig against a local
// httptest server and asserts the exact request shapes labctl sends.
func TestCloudflareWrites(t *testing.T) {
	var lastMethod, lastPath, lastBody string
	srv := newCFTestServer(t, &lastMethod, &lastPath, &lastBody)
	oldBase := cloudflareAPIBase
	cloudflareAPIBase = srv.URL
	t.Cleanup(func() { cloudflareAPIBase = oldBase })

	// 1. absent CNAME + apply -> POST create
	srv.setQueue(`{"success": true, "result": []}`, `{"success": true, "result": {"id": "rec1"}}`)
	status, msg, err := EnsureCname("tok", "zone1", "canary.example.test", "tun.cfargotunnel.com", true)
	if err != nil || status != "created" {
		t.Fatalf("ensure/missing: status=%q err=%v msg=%q", status, err, msg)
	}
	if msg != "canary.example.test: CNAME created -> tun.cfargotunnel.com" {
		t.Fatalf("created message: %q", msg)
	}
	if lastMethod != "POST" || lastPath != "/zones/zone1/dns_records" {
		t.Fatalf("create call: %s %s", lastMethod, lastPath)
	}
	if !strings.Contains(lastBody, `"proxied":true`) || !strings.Contains(lastBody, `"content":"tun.cfargotunnel.com"`) {
		t.Fatalf("create body: %s", lastBody)
	}

	// 2. read-only on absence -> "missing", no second call
	srv.setQueue(`{"success": true, "result": []}`)
	status, msg, err = EnsureCname("tok", "zone1", "h.example.test", "target", false)
	if err != nil || status != "missing" || msg != "h.example.test: CNAME absent (want -> target)" {
		t.Fatalf("ensure/read-only-missing: %q %v %q", status, err, msg)
	}

	// 3. drift + apply -> PUT retarget with the record id
	srv.setQueue(`{"success": true, "result": [{"id": "rec9", "content": "old.target"}]}`, `{"success": true, "result": {"id": "rec9"}}`)
	status, msg, err = EnsureCname("tok", "zone1", "h.example.test", "target", true)
	if err != nil || status != "fixed" || msg != "h.example.test: CNAME retargeted old.target -> target" {
		t.Fatalf("ensure/drift: %q %v %q", status, err, msg)
	}
	if lastMethod != "PUT" || lastPath != "/zones/zone1/dns_records/rec9" {
		t.Fatalf("retarget call: %s %s", lastMethod, lastPath)
	}

	// 4. content matches -> ok, single GET
	srv.setQueue(`{"success": true, "result": [{"id": "rec9", "content": "target"}]}`)
	status, msg, err = EnsureCname("tok", "zone1", "h.example.test", "target", false)
	if err != nil || status != "ok" || msg != "h.example.test: ok" {
		t.Fatalf("ensure/ok: %q %v %q", status, err, msg)
	}

	// 5. PutTunnelConfig: ingress + terminating 404 catch-all
	srv.setQueue(`{"success": true, "result": {}}`)
	if err := PutTunnelConfig("tok", "acct1", "tun1", []any{
		map[string]any{"hostname": "a.example.test", "service": "http://a.svc:80"},
	}); err != nil {
		t.Fatalf("put tunnel: %v", err)
	}
	if lastMethod != "PUT" || lastPath != "/accounts/acct1/cfd_tunnel/tun1/configurations" {
		t.Fatalf("tunnel call: %s %s", lastMethod, lastPath)
	}
	if !strings.Contains(lastBody, `"http_status:404"`) ||
		!strings.Contains(lastBody, `"hostname":"a.example.test"`) {
		t.Fatalf("tunnel body: %s", lastBody)
	}
}

// cfTestServer is a minimal Cloudflare API double: pops queued responses
// and records the last request.
type cfTestServer struct {
	*httptest.Server
	queueMu sync.Mutex
	queue   []string
	method  *string
	path    *string
	body    *string
}

func newCFTestServer(t *testing.T, method, path, body *string) *cfTestServer {
	t.Helper()
	s := &cfTestServer{method: method, path: path, body: body}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		s.queueMu.Lock()
		defer s.queueMu.Unlock()
		*method = r.Method
		*path = r.URL.Path
		*body = string(raw)
		resp := "{}"
		if len(s.queue) > 0 {
			resp = s.queue[0]
			s.queue = s.queue[1:]
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, resp)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *cfTestServer) setQueue(responses ...string) {
	s.queueMu.Lock()
	defer s.queueMu.Unlock()
	s.queue = append(s.queue, responses...)
}

// TestMiniYamlPyyamlStyle is the piece-3 regression: labctl's
// set_service_field rewrites the whole registry with yaml.safe_dump —
// list items same-indent, flow lists become block lists, and long plain
// scalars fold across lines at deeper indent INSIDE list items. The
// parser must read that style (it is what the authoritative engine
// writes after any deploy of a disabled service).
func TestMiniYamlPyyamlStyle(t *testing.T) {
	src := `cloudflare:
  account_id: acct
  tunnel_id: tun
  tunnel_name: lab
domains:
  example.test:
    zone_id: z1
services:
  oc:
    repo: oc
    port: 3000
    namespace: sos-lab
    enabled: true
    args:
    - serve
    - --foreground
    parity:
    - for l in a.so b.so c.so; do
      [ -e "/usr/lib/$l" ] || { echo "missing $l"; exit 1; }; done
    env:
      KUBECONFIG: ''
    secrets:
    - UI_PASSWORD
`
	v, err := parseMiniYAML(src)
	if err != nil {
		t.Fatalf("pyyaml-style registry rejected: %v", err)
	}
	svc := asMap(asMap(asMap(v)["services"])["oc"])
	if svc == nil {
		t.Fatal("oc missing")
	}
	args := asList(svc["args"])
	if len(args) != 2 || asString(args[0]) != "serve" || asString(args[1]) != "--foreground" {
		t.Fatalf("args: %v", args)
	}
	par := asList(svc["parity"])
	if len(par) != 1 {
		t.Fatalf("parity should fold to ONE scalar, got %d: %v", len(par), par)
	}
	want := `for l in a.so b.so c.so; do [ -e "/usr/lib/$l" ] || { echo "missing $l"; exit 1; }; done`
	if asString(par[0]) != want {
		t.Fatalf("parity fold mismatch\nGOT : %q\nWANT: %q", asString(par[0]), want)
	}
	if asString(svc["env"].(map[string]any)["KUBECONFIG"]) != "" {
		t.Fatalf("KUBECONFIG should be empty string")
	}
}

// TestSecretsDirOverride guards the WO-9 reference-not-copy contract: the
// slug and CF-token readers resolve through the site secrets_dir override
// exactly once (no doubled "secrets" segment).
func TestSecretsDirOverride(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cloudflare.env"), []byte("CF_API_TOKEN=x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dashboard.env"), []byte("DASHBOARD_SLUG=abc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tok, err := LoadCloudflareToken(dir)
	if err != nil || tok != "x" {
		t.Fatalf("cf token: %q %v", tok, err)
	}
	slug, err := labDashboardSlug(dir)
	if err != nil || slug != "abc" {
		t.Fatalf("slug: %q %v", slug, err)
	}
	missing := filepath.Join(dir, "secrets")
	if _, err := LoadCloudflareToken(missing); err == nil {
		t.Fatal("missing secrets dir must error")
	}
}
