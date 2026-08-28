package fleet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSiteTunnelCreate drives the WO-15 fresh-install core against the
// httptest CF double: account -> tunnel create (token inline) -> zone
// lookup -> registry record -> tunnel.env stored 0600, registry still
// validates.
func TestSiteTunnelCreate(t *testing.T) {
	scratch := t.TempDir()
	lab := filepath.Join(scratch, "lab")
	copyDir(t, labFix, lab)
	t.Setenv("FLEET_SECRETS_HOME", scratch)
	if err := os.WriteFile(filepath.Join(lab, "secrets-not-used-marker"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// the CF token for the site comes from the secrets home (WO-14)
	if err := os.MkdirAll(filepath.Join(scratch, "fix"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scratch, "fix", "cloudflare.env"), []byte("CF_API_TOKEN=tok\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var method, path, body string
	srv := newCFTestServer(t, &method, &path, &body)
	oldBase := cloudflareAPIBase
	cloudflareAPIBase = srv.URL
	t.Cleanup(func() { cloudflareAPIBase = oldBase })

	srv.setQueue(
		`{"success": true, "result": [{"id": "acct9", "name": "main"}]}`,
		`{"success": true, "result": {"id": "tun9", "name": "fix-tunnel", "token": "tt9"}}`,
		`{"success": true, "result": [{"id": "zone9"}]}`,
	)

	p := Paths{Root: scratch, Journal: filepath.Join(scratch, "journal.log")}
	site := Site{Name: "fix", Engine: "fleet", LabRoot: lab, Namespace: "sos-lab", Access: "in-cluster"}
	if err := runSiteTunnelCreate(p, site, "example.test"); err != nil {
		t.Fatalf("runSiteTunnelCreate: %v", err)
	}

	// registry recorded tunnel ids from the skeleton TODO markers
	reg, err := os.ReadFile(filepath.Join(lab, "config", "registry.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(reg), "tunnel_id: tun9") ||
		!strings.Contains(string(reg), "tunnel_name: fix-tunnel") {
		t.Fatalf("registry not recorded:\n%s", reg)
	}

	// tunnel token stored 0600 in the secrets home, never printed
	tok, err := os.ReadFile(filepath.Join(scratch, "fix", "tunnel.env"))
	if err != nil || !strings.Contains(string(tok), "TUNNEL_TOKEN=tt9") {
		t.Fatalf("tunnel.env: %q %v", tok, err)
	}
	if info, err := os.Stat(filepath.Join(scratch, "fix", "tunnel.env")); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("tunnel.env mode: %v", err)
	}

	// journal line written (machine-inspectable, no token values)
	jr, err := os.ReadFile(p.Journal)
	if err != nil || !strings.Contains(string(jr), "# site-tunnel-create wo=") {
		t.Fatalf("journal: %q %v", jr, err)
	}
	if strings.Contains(string(jr), "tt9") {
		t.Fatal("journal must never contain the tunnel token")
	}

	// the exact CF call shapes: accounts GET, tunnel POST (config_src
	// cloudflare), zone GET by name
	if !strings.Contains(body, `"config_src":"cloudflare"`) && method != "GET" {
		t.Fatalf("expected the last recorded POST to be the tunnel create, got %s %s", method, path)
	}
}
