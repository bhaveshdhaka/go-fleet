package fleet

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// site new (WO-15): scaffold a BRAND-NEW fleet-managed site — the
// fresh-install path for operators who have no sos-lab predecessor to
// migrate. Pure file work, zero cluster/CF calls (that is
// `site tunnel create`): lab root skeleton, embedded templates, SITES.yaml
// entry, secrets home dir. --dry-run renders the byte-stable plan and
// writes nothing (same contract as every other fleet planner).

//go:embed templates/site/*
var siteTemplateFS embed.FS

var siteNameTemplateOrder = []string{
	"docker-registry.yaml",
	"cloudflared.yaml",
	"gatus.yaml",
	"dashboard-nginx.conf",
	"dashboard-render.py",
}

func cmdSiteNew(args []string) int {
	p, rc := mustPaths()
	if rc != 0 {
		return rc
	}
	name := ""
	ns := ""
	access := ""
	domain := ""
	dryRun := false
	// flag pass: site new takes exactly one positional
	var positional []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--dry-run":
			dryRun = true
		case "--namespace":
			if i+1 >= len(args) {
				return failf("--namespace requires a value")
			}
			ns = args[i+1]
			i++
		case "--access":
			if i+1 >= len(args) {
				return failf("--access requires a value")
			}
			access = args[i+1]
			i++
		case "--domain":
			if i+1 >= len(args) {
				return failf("--domain requires a value")
			}
			domain = args[i+1]
			i++
		default:
			if strings.HasPrefix(args[i], "-") {
				return failf("unknown flag %q for site new", args[i])
			}
			positional = append(positional, args[i])
		}
	}
	if len(positional) != 1 {
		return failf("usage: fleet site new <name> [--namespace NS] [--access in-cluster|kubeconfig:<path>] [--domain D] [--dry-run]")
	}
	name = positional[0]
	if !siteNameRe.MatchString(name) {
		return failf("site name must match %s", siteNameRe.String())
	}
	if ns == "" {
		ns = "sos-lab"
	}
	if access == "" {
		access = "in-cluster"
	}
	if !validSiteAccess(access) {
		return failf("site '%s': invalid access mode '%s'", name, access)
	}
	labRootRel := filepath.Join("ops", "sites", name)
	dst := filepath.Join(p.Root, labRootRel)

	sites, err := LoadSites(p)
	if err != nil {
		return failf("sites registry unreadable: %v", err)
	}
	if _, found := getSite(sites, name); found {
		return failf("site '%s' is already registered in ops/SITES.yaml", name)
	}
	if _, err := os.Stat(dst); err == nil {
		return failf("site data dir already exists: %s", dst)
	}
	secretsHome := Site{Name: name}.secretsDir(p.Root)

	plan := siteNewPlan(name, ns, access, domain, labRootRel, secretsHome)
	if dryRun {
		fmt.Print(plan)
		return 0
	}

	// scaffold
	if err := os.MkdirAll(filepath.Join(dst, "config"), 0o755); err != nil {
		return failf("%v", err)
	}
	if err := os.MkdirAll(filepath.Join(dst, "state"), 0o755); err != nil {
		return failf("%v", err)
	}
	if err := os.MkdirAll(filepath.Join(dst, "templates"), 0o755); err != nil {
		return failf("%v", err)
	}
	reg := siteNewRegistry(name, domain)
	if err := os.WriteFile(filepath.Join(dst, "config", "registry.yaml"), []byte(reg), 0o644); err != nil {
		return failf("%v", err)
	}
	for _, f := range []string{"deployed.json", "builds.json"} {
		if err := os.WriteFile(filepath.Join(dst, "state", f), []byte("{}\n"), 0o644); err != nil {
			return failf("%v", err)
		}
	}
	for _, tn := range siteNameTemplateOrder {
		raw, err := siteTemplateFS.ReadFile("templates/site/" + tn)
		if err != nil {
			return failf("embedded template %s: %v", tn, err)
		}
		// the skeletons namespace-annotate the reference site; the new
		// site's namespace is substituted on write
		out := strings.ReplaceAll(string(raw), "namespace: sos-lab\n", "namespace: "+ns+"\n")
		if err := os.WriteFile(filepath.Join(dst, "templates", tn), []byte(out), 0o644); err != nil {
			return failf("%v", err)
		}
	}
	if err := os.MkdirAll(secretsHome, 0o700); err != nil {
		return failf("%v", err)
	}
	if err := sitesYamlCutover(p, name, Site{Namespace: ns, Access: access}, false); err != nil {
		os.RemoveAll(dst)
		return failf("%v", err)
	}

	AppendJournal(p.Journal, fmt.Sprintf(
		"# site-new wo=%s site=%s namespace=%s access=%s templates=%d domain=%s",
		os.Getenv("FLEET_WO"), name, ns, access, len(siteNameTemplateOrder), domain))

	fmt.Printf("SITE NEW site=%s namespace=%s access=%s lab_root=%s templates=%d secrets_home=%s\n",
		name, ns, access, labRootRel, len(siteNameTemplateOrder), secretsHome)
	fmt.Printf("next: fleet site tunnel create %s --domain <your-domain>  (then fill cloudflare.account_id)\n", name)
	return 0
}

// siteNewPlan renders the byte-stable dry-run plan: same content the
// mutating path creates, no timestamps, no variability.
func siteNewPlan(name, ns, access, domain, labRootRel, secretsHome string) string {
	var b strings.Builder
	files := []string{
		filepath.Join(labRootRel, "config", "registry.yaml"),
		filepath.Join(labRootRel, "state", "deployed.json"),
		filepath.Join(labRootRel, "state", "builds.json"),
	}
	for _, tn := range siteNameTemplateOrder {
		files = append(files, filepath.Join(labRootRel, "templates", tn))
	}
	sort.Strings(files)
	for _, f := range files {
		fmt.Fprintf(&b, "PLAN FILE %s\n", f)
	}
	fmt.Fprintf(&b, "PLAN DIR %s (mode 0700)\n", secretsHome)
	fmt.Fprintf(&b, "PLAN SITES.YAML + name=%s engine=fleet namespace=%s access=%s\n", name, ns, access)
	if domain != "" {
		fmt.Fprintf(&b, "PLAN REGISTRY domains.%s reserved (zone_id via site tunnel create)\n", domain)
	}
	fmt.Fprintf(&b, "SITE NEW PLAN site=%s files=%d byte_stable=true\n", name, len(files))
	return b.String()
}

// siteNewRegistry renders the registry skeleton. TODO markers are
// deliberate: they pass validation (non-empty) while making `ops doctor`
// and every read verb show exactly what the operator still owes the site.
func siteNewRegistry(name, domain string) string {
	var b strings.Builder
	b.WriteString("# site registry — deployment INTENT (WO-15 skeleton)\n")
	b.WriteString("# cloudflare.* and domains.*.zone_id are filled by\n")
	b.WriteString("# `fleet site tunnel create <name> --domain <domain>` + the operator\n")
	b.WriteString("# (account_id comes from the Cloudflare dashboard or GET /accounts).\n")
	b.WriteString("cloudflare:\n")
	b.WriteString("  account_id: TODO_ACCOUNT_ID\n")
	b.WriteString("  tunnel_id: TODO_SITE_TUNNEL_CREATE\n")
	b.WriteString("  tunnel_name: " + name + "\n")
	b.WriteString("domains:\n")
	if domain != "" {
		b.WriteString("  " + domain + ":\n")
		b.WriteString("    zone_id: TODO_SITE_TUNNEL_CREATE\n")
		b.WriteString("    purpose: primary\n")
	}
	b.WriteString("services: {}\n")
	return b.String()
}
