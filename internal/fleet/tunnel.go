package fleet

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// site tunnel create (WO-15): the fresh-install path's ONE online step.
// Uses the site's CF API token (secrets home, cloudflare.env) to:
//   1. create the named tunnel (config_src: cloudflare — ingress is
//      reconciled by `ops dns --apply` from the registry, never by hand),
//   2. store the TUNNEL token for the cloudflared deployment
//      (secrets home tunnel.env, 0600 — never printed),
//   3. record tunnel_id/tunnel_name in the site registry,
//   4. with --domain: resolve the zone id via GET /zones?name= and record
//      domains.<domain>.zone_id.
// Then `infra deploy` stands up cloudflared+registry+gatus+dashboard and
// `site canary` proves the whole loop.
func cmdSiteTunnel(args []string) int {
	p, rc := mustPaths()
	if rc != 0 {
		return rc
	}
	action := ""
	siteName := ""
	domain := ""
	var positional []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--domain":
			if i+1 >= len(args) {
				return failf("--domain requires a value")
			}
			domain = args[i+1]
			i++
		default:
			if strings.HasPrefix(args[i], "-") {
				return failf("unknown flag %q for site tunnel", args[i])
			}
			positional = append(positional, args[i])
		}
	}
	if len(positional) < 1 {
		return failf("usage: fleet site tunnel create <site> --domain <domain>")
	}
	action = positional[0]
	if action != "create" {
		return failf("unknown site tunnel action '%s' (create)", action)
	}
	if len(positional) != 2 {
		return failf("usage: fleet site tunnel create <site> --domain <domain>")
	}
	siteName = positional[1]
	if domain == "" {
		return failf("--domain is required (the primary zone this site serves)")
	}

	sites, err := LoadSites(p)
	if err != nil {
		return failf("sites registry unreadable: %v", err)
	}
	site, found := getSite(sites, siteName)
	if !found {
		return failf("unknown site '%s' (fleet site new first)", siteName)
	}
	if !validSiteAccess(site.Access) {
		return failf("site '%s': invalid access mode '%s'", site.Name, site.Access)
	}
	if err := runSiteTunnelCreate(p, site, domain); err != nil {
		return failf("%v", err)
	}

	fmt.Printf("TUNNEL CREATED site=%s domain=%s token_stored=tunnel.env\n", siteName, domain)
	fmt.Printf("next: fill cloudflare.account_id in %s, then ./scripts/fleet infra deploy --site %s\n",
		filepath.Join(site.LabRoot, "config", "registry.yaml"), siteName)
	return 0
}

// runSiteTunnelCreate is the testable core: CF calls, token storage,
// registry record, journal. The CF endpoint is the package var (tests
// point it at an httptest server).
func runSiteTunnelCreate(p Paths, site Site, domain string) error {
	token, err := LoadCloudflareToken(site.secretsDir(p.Root))
	if err != nil {
		return err
	}

	// 1. resolve the account (cloudflareAPI returns the unwrapped result)
	accRes, err := cloudflareAPI(token, "GET", "/accounts")
	if err != nil {
		return err
	}
	var accounts []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(accRes, &accounts); err != nil || len(accounts) == 0 {
		return fmt.Errorf("CF /accounts returned no usable account")
	}
	accountID := accounts[0].ID

	// 2. create the tunnel
	tunRes, err := cloudflareAPIBody(token, "POST",
		"/accounts/"+accountID+"/cfd_tunnel",
		map[string]any{"name": site.Name + "-tunnel", "config_src": "cloudflare"})
	if err != nil {
		return err
	}
	var tunnel struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Token  string `json:"token"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(tunRes, &tunnel); err != nil || tunnel.ID == "" {
		return fmt.Errorf("CF tunnel create returned no id")
	}
	if tunnel.Token == "" {
		tokRes, err := cloudflareAPI(token, "GET",
			"/accounts/"+accountID+"/cfd_tunnel/"+tunnel.ID+"/token")
		if err != nil {
			return err
		}
		if err := json.Unmarshal(tokRes, &tunnel.Token); err != nil || tunnel.Token == "" {
			return fmt.Errorf("CF tunnel token unavailable")
		}
	}

	// 3. store the tunnel token for the cloudflared deployment (0600)
	secretsDir := site.secretsDir(p.Root)
	if err := os.MkdirAll(secretsDir, 0o700); err != nil {
		return err
	}
	tunnelEnv := filepath.Join(secretsDir, "tunnel.env")
	if err := os.WriteFile(tunnelEnv, []byte("TUNNEL_TOKEN="+tunnel.Token+"\n"), 0o600); err != nil {
		return err
	}

	// 4. record tunnel ids + domain zone in the site registry
	zoneID := ""
	zRes, err := cloudflareAPI(token, "GET", "/zones?name="+domain)
	if err == nil {
		var zones []struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(zRes, &zones) == nil && len(zones) > 0 {
			zoneID = zones[0].ID
		}
	}
	if err := labTunnelRecord(site, p, tunnel.ID, tunnel.Name, domain, zoneID); err != nil {
		return err
	}

	AppendJournal(p.Journal, fmt.Sprintf(
		"# site-tunnel-create wo=%s site=%s tunnel_id=%s tunnel_name=%s domain=%s zone_recorded=%v token_stored=tunnel.env",
		os.Getenv("FLEET_WO"), site.Name, tunnel.ID, tunnel.Name, domain, zoneID != ""))
	return nil
}

// labTunnelRecord anchors cloudflare.tunnel_id/tunnel_name and (when the
// domain is a TODO skeleton entry) domains.<domain>.zone_id, then
// re-validates the registry exactly like every other registry writer.
func labTunnelRecord(site Site, p Paths, tunnelID, tunnelName, domain, zoneID string) error {
	path := filepath.Join(site.LabRootAbs(p.Root), "config", "registry.yaml")
	lines, err := readLines(path)
	if err != nil {
		return err
	}
	var out []string
	inCF := false
	cfDone := false
	domainDone := false
	for _, ln := range lines {
		// the cloudflare section spans its indented children; a 0-indent
		// (top-level) key ends it. Comments/blank lines never do.
		if inCF {
			t := strings.TrimSpace(ln)
			if t != "" && !strings.HasPrefix(ln, "#") && !strings.HasPrefix(ln, " ") {
				inCF = false
			}
		}
		if strings.TrimRight(ln, " ") == "cloudflare:" {
			inCF = true
		}
		if inCF {
			if strings.HasPrefix(strings.TrimSpace(ln), "tunnel_id:") {
				out = append(out, "  tunnel_id: "+tunnelID)
				cfDone = true
				continue
			}
			if strings.HasPrefix(strings.TrimSpace(ln), "tunnel_name:") {
				out = append(out, "  tunnel_name: "+tunnelName)
				continue
			}
		}
		if zoneID != "" && !domainDone && domain != "" &&
			strings.TrimSpace(ln) == "zone_id: TODO_SITE_TUNNEL_CREATE" {
			out = append(out, ln[:len(ln)-len("TODO_SITE_TUNNEL_CREATE")]+zoneID)
			domainDone = true
			continue
		}
		out = append(out, ln)
	}
	if !cfDone {
		return fmt.Errorf("registry.yaml: no cloudflare.tunnel_id line to anchor")
	}
	text := strings.Join(out, "\n")
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	tmp := path + ".fleet.tmp"
	if err := os.WriteFile(tmp, []byte(text), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	// re-validate by full parse (same contract as ops register/monitor)
	if _, err := LoadLabView(site, p.Root); err != nil {
		return fmt.Errorf("registry edit failed validation: %v", err)
	}
	return nil
}
