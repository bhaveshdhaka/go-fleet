package fleet

import (
	"fmt"
	"os"
	"path/filepath"
)

// site canary (WO-15): the "prove your install" drill. Drives the FULL
// ship path against the real site — register → build (kaniko) → deploy →
// public verify → remove — with a tiny committed Go app (apps/canary).
// Every step is the SAME cmdOps dispatch an agent would run (zero new
// mutation logic), so the canary tests the product through its own front
// door. Anything fails -> exact FLEET ERROR, no cleanup shortcuts.
func cmdSiteCanary(args []string) int {
	p, rc := mustPaths()
	if rc != 0 {
		return rc
	}
	siteName := ""
	domain := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--site":
			if i+1 >= len(args) {
				return failf("--site requires a value")
			}
			siteName = args[i+1]
			i++
		case "--domain":
			if i+1 >= len(args) {
				return failf("--domain requires a value")
			}
			domain = args[i+1]
			i++
		default:
			return failf("unknown argument %q for site canary", args[i])
		}
	}

	sites, err := LoadSites(p)
	if err != nil {
		return failf("sites registry unreadable: %v", err)
	}
	if len(sites) == 0 {
		return failf("no sites registered in ops/SITES.yaml")
	}
	var site Site
	if siteName != "" {
		var found bool
		site, found = getSite(sites, siteName)
		if !found {
			return failf("unknown site '%s'", siteName)
		}
	} else if len(sites) == 1 {
		site = sites[0]
	} else {
		return failf("multiple sites registered; pass --site")
	}

	// canary host = canary.<parent domain> (--domain wins; default is the
	// first registry domain, sorted)
	domains := labRegistryDomains(p, site)
	if len(domains) == 0 {
		return failf("site '%s' has no domains in its registry (site tunnel create --domain fills it)", site.Name)
	}
	if domain == "" {
		domain = domains[0]
	}
	if !domainKnown(domains, domain) {
		return failf("domain '%s' is not in site '%s' registry domains", domain, site.Name)
	}
	host := "canary." + domain

	ops := func(args ...string) int {
		full := append([]string{"--site", site.Name}, args...)
		return cmdOps(full)
	}

	// step 1 — register (idempotent: a leftover canary is re-registered by
	// remove --unregister at the end of every successful run). NOTE: this
	// dirties the repo by design, so the canary build runs --allow-dirty
	// and the drill is journaled end to end.
	if rc := ops("register", "canary", "--port", "8080", "--host", host,
		"--repo", "fleet/apps/canary"); rc != 0 {
		return rc
	}
	// step 2 — build (kaniko)
	if rc := ops("build", "canary", "--allow-dirty"); rc != 0 {
		return rc
	}
	// step 3 — deploy (secret/manifests/rollout/dns/tunnel/monitor/state)
	if rc := ops("deploy", "canary"); rc != 0 {
		return rc
	}
	// step 4 — public smoke test
	if rc := ops("verify", "canary", "--expect", "200"); rc != 0 {
		return rc
	}
	// step 5 — teardown, back to a clean registry/state
	if rc := ops("remove", "canary", "--unregister", "--delete-data"); rc != 0 {
		return rc
	}

	AppendJournal(p.Journal, fmt.Sprintf(
		"# site-canary wo=%s site=%s host=%s result=PASS",
		os.Getenv("FLEET_WO"), site.Name, host))

	fmt.Printf("CANARY PASS site=%s host=%s\n", site.Name, host)
	fmt.Printf("your site builds, ships, serves publicly and tears down cleanly — start a real project with ./scripts/fleet onboard <name>\n")
	return 0
}

// domainKnown reports whether d is among the registry domains.
func domainKnown(domains []string, d string) bool {
	for _, k := range domains {
		if k == d {
			return true
		}
	}
	return false
}

// labRegistryDomains returns the registry's domain keys, sorted (the
// first is the canary's default parent domain).
func labRegistryDomains(p Paths, site Site) []string {
	raw, err := os.ReadFile(filepath.Join(site.LabRootAbs(p.Root), "config", "registry.yaml"))
	if err != nil {
		return nil
	}
	top, err := parseMiniYAML(string(raw))
	if err != nil {
		return nil
	}
	var out []string
	for k := range asMap(asMap(top)["domains"]) {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}
