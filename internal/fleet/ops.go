package fleet

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// ops (WO-7): read-only observation of a managed site, byte-compatible
// with the sos-lab engine's own status/doctor. ZERO mutations.

func cmdOps(args []string) int {
	p, rc := mustPaths()
	if rc != 0 {
		return rc
	}
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	if sub != "status" && sub != "doctor" {
		fmt.Fprintln(os.Stderr, "usage: fleet ops <status|doctor> [--site NAME] [--json] [service]")
		return 2
	}
	rest := args[1:]
	siteName, jsonMode := "", false
	var positional []string
	for _, a := range rest {
		switch {
		case a == "--json":
			jsonMode = true
		case strings.HasPrefix(a, "--site="):
			siteName = strings.TrimPrefix(a, "--site=")
		case a == "--site":
			// consumed with its value below
			positional = append(positional, a)
		default:
			positional = append(positional, a)
		}
	}
	// handle "--site NAME" two-token form
	var tail []string
	for i := 0; i < len(positional); i++ {
		if positional[i] == "--site" {
			if i+1 >= len(positional) {
				return failf("--site requires a value")
			}
			siteName = positional[i+1]
			i++
			continue
		}
		tail = append(tail, positional[i])
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
	if site.Engine != "sos-lab" {
		return failf("site '%s': engine '%s' not supported yet", site.Name, site.Engine)
	}
	if !validSiteAccess(site.Access) {
		return failf("site '%s': invalid access mode '%s'", site.Name, site.Access)
	}

	lv, err := LoadLabView(site, p.Root)
	if err != nil {
		return failf("%v", err)
	}
	runner, cleanup, err := newKubectlRunner(site, p.Root)
	if err != nil {
		return failf("%v", err)
	}
	defer cleanup()

	switch sub {
	case "status":
		return opsStatus(lv, runner, tail)
	case "doctor":
		return opsDoctor(lv, runner, p.Root, jsonMode)
	}
	return 2
}

// opsStatus mirrors labctl.cli.cmd_status byte-for-byte: the nodes call is
// a discarded reachability probe (python prints nothing), pods output is
// printed with print() semantics (one extra trailing newline), and the
// services table is ljust-padded in every column including the last.
func opsStatus(lv *LabView, r *kubectlRunner, args []string) int {
	fmt.Println("=== cluster ===")
	nodesOut, nodesErr, nodesRc := r.runStd("get", "nodes", "-o", "wide")
	if nodesRc != 0 {
		fmt.Fprintf(os.Stderr, "%s", nodesErr)
		_ = nodesOut
		return 1
	}
	fmt.Println("\n=== pods (sos-lab) ===")
	podsOut, podsErr, _ := r.runStd("-n", lv.Site.Namespace, "get", "pods", "-o", "wide")
	if podsOut != "" {
		fmt.Println(podsOut)
	} else {
		fmt.Println(podsErr)
	}
	fmt.Println("\n=== services (registry) ===")
	type row struct{ cols [6]string }
	rows := []row{}
	for _, name := range lv.LabServiceNames() {
		svc := lv.LabServices()[name]
		stateTxt := "disabled"
		if asBool(svc["enabled"]) {
			stateTxt = "enabled"
		}
		host := "-"
		if h := asString(svc["host"]); h != "" {
			host = "https://" + h
		}
		rows = append(rows, row{[6]string{
			name, stateTxt, asString(svc["port"]),
			lv.DeployedTag(name), lv.StatusSHA(name), host,
		}})
	}
	header := [6]string{"service", "state", "port", "deployed-tag", "sha", "url"}
	widths := [6]int{}
	for i, h := range header {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, c := range row.cols {
			if len(c) > widths[i] {
				widths[i] = len(c)
			}
		}
	}
	printTableRow(header[:], widths[:])
	for _, row := range rows {
		c := row.cols
		printTableRow(c[:], widths[:])
	}
	if len(args) > 0 && args[0] != "" {
		name := args[0]
		svc, ok := lv.LabServices()[name]
		if !ok {
			return failf("unknown service '%s'", name)
		}
		ns := asString(svc["namespace"])
		fmt.Printf("\n=== detail: %s (%s) ===\n", name, ns)
		out, _, _ := r.runStd("-n", ns, "get", "deployment,svc,endpoints,pvc", "-l", "app="+name)
		fmt.Print(out)
	}
	return 0
}

func printTableRow(cols []string, widths []int) {
	parts := make([]string, len(cols))
	for i, c := range cols {
		parts[i] = c + strings.Repeat(" ", widths[i]-len(c))
	}
	fmt.Println(strings.Join(parts, "  "))
}

// --- doctor -----------------------------------------------------------

type doctorCheck struct {
	Check  string `json:"check"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
	Status string `json:"status"`
}

func addCheck(checks *[]doctorCheck, name string, ok bool, detail, status string) {
	if status == "" {
		if ok {
			status = "ok"
		} else {
			status = "fail"
		}
	}
	if status == "skip" {
		ok = true
	}
	*checks = append(*checks, doctorCheck{Check: name, OK: ok, Detail: detail, Status: status})
}

func opsDoctor(lv *LabView, r *kubectlRunner, root string, jsonMode bool) int {
	checks := []doctorCheck{}
	add := func(name string, ok bool, detail, status string) {
		addCheck(&checks, name, ok, detail, status)
	}

	svcMap := lv.LabServices()
	add("registry.yaml parses", true, fmt.Sprintf("%d services", len(svcMap)), "")

	add("cluster reachable", r.ClusterReachable(), "", "")
	if !checks[len(checks)-1].OK {
		return doctorFinish(checks, jsonMode)
	}

	add("node ready", r.NodeReady(), "", "")
	add("cloudflared running", r.PodRunning(lv.Site.Namespace, "cloudflared"), "", "")
	add("gatus running", r.PodRunning(lv.Site.Namespace, "gatus"), "", "")
	add("sos-dashboard running", r.PodRunning(lv.Site.Namespace, "sos-dashboard"), "", "")

	if present := r.BuilderImagePresent(LabKanikoImage); present == nil {
		add("builder image cached", true, "crictl unavailable here — skipped", "skip")
	} else if *present {
		add("builder image cached", true, LabKanikoImage, "")
	} else {
		add("builder image cached", true,
			LabKanikoImage+" not in node cache — next build pulls on demand", "warn")
	}

	cfCfg := asMap(lv.Registry["cloudflare"])
	token, tokenErr := LoadCloudflareToken(lv.Site.LabRootAbs(root))
	if tokenErr != nil {
		add("tunnel healthy", false, tokenErr.Error(), "")
	} else {
		st, err := TunnelStatus(token, asString(cfCfg["account_id"]), asString(cfCfg["tunnel_id"]))
		if err != nil {
			add("tunnel healthy", false, err.Error(), "")
		} else {
			add("tunnel healthy", st == "healthy", "status="+st, "")
		}
	}

	routed := lv.RoutedServices()
	wantHosts := map[string]bool{}
	for _, rs := range routed {
		wantHosts[asString(rs.Svc["host"])] = true
	}
	var liveIngress []string
	if tokenErr == nil {
		liveIngress, _ = TunnelIngress(token, asString(cfCfg["account_id"]), asString(cfCfg["tunnel_id"]))
	}
	if tokenErr != nil && liveIngress == nil {
		add("tunnel ingress matches registry", false, "could not read tunnel config", "")
	} else {
		liveSet := map[string]bool{}
		for _, h := range liveIngress {
			liveSet[h] = true
		}
		var missing, extra []string
		for h := range wantHosts {
			if !liveSet[h] {
				missing = append(missing, h)
			}
		}
		for h := range liveSet {
			if !wantHosts[h] {
				extra = append(extra, h)
			}
		}
		sort.Strings(missing)
		sort.Strings(extra)
		if len(missing) == 0 && len(extra) == 0 {
			add("tunnel ingress matches registry", true, "", "")
		} else {
			add("tunnel ingress matches registry", false,
				fmt.Sprintf("missing=%v extra=%v", missing, extra), "")
		}
	}

	for _, name := range lv.LabServiceNames() {
		errTxt := lv.CheckSecrets(name, svcMap[name], root)
		if errTxt == "" {
			add("secrets/"+name+".env", true, "ok", "")
		} else {
			add("secrets/"+name+".env", false, errTxt, "")
		}
	}

	for _, entryName := range sortedKeys(lv.Deployed) {
		svc := svcMap[entryName]
		ns := lv.Site.Namespace
		if svc != nil && asString(svc["namespace"]) != "" {
			ns = asString(svc["namespace"])
		}
		want := stateEntry(lv.Deployed, entryName, "image")
		if want == "" {
			if tag := stateEntry(lv.Deployed, entryName, "tag"); tag != "" {
				want = LabRegistryHost + "/" + entryName + ":" + tag
			}
		}
		live := r.DeploymentImage(ns, entryName)
		switch {
		case live == "":
			add("deployed/"+entryName, false, "state says deployed but deployment not found", "")
		case live != want:
			add("deployed/"+entryName, false,
				fmt.Sprintf("running %s != deployed intent %s", live, want), "")
		default:
			add("deployed/"+entryName, true, want, "")
		}
	}

	// parity/<svc>: declared sh assertions exec'd inside running pods
	for _, name := range lv.LabServiceNames() {
		assertions := asList(svcMap[name]["parity"])
		if len(assertions) == 0 {
			continue
		}
		ns := asString(svcMap[name]["namespace"])
		if ns == "" {
			ns = lv.Site.Namespace
		}
		if r.DeploymentImage(ns, name) == "" {
			add("parity/"+name, false, "deployment not found", "")
			continue
		}
		var failures []string
		for _, a := range assertions {
			script := asString(a)
			rc, out := r.ExecInPod(ns, name, script)
			if rc != 0 {
				why := firstLine(out)
				if why == "" {
					why = fmt.Sprintf("exit %d", rc)
				}
				failures = append(failures, truncate(script, 60)+"… -> "+why)
			}
		}
		if len(failures) > 0 {
			add("parity/"+name, false, strings.Join(failures, "; "), "")
		} else {
			add("parity/"+name, true, fmt.Sprintf("%d assertion(s) hold", len(assertions)), "")
		}
	}

	// lifecycle warnings
	builds := lv.Builds
	deployed := lv.Deployed
	for _, name := range lv.LabServiceNames() {
		svc := svcMap[name]
		if asString(svc["repo"]) == "" {
			continue
		}
		buildTag := stateEntry(builds, name, "tag")
		deployedEntry := asMap(deployed[name]) != nil
		deployedTag := stateEntry(deployed, name, "tag")
		switch {
		case buildTag == "" && !deployedEntry:
			add("lifecycle/"+name, true, "registered but never built/deployed", "warn")
		case buildTag != "" && (!deployedEntry || deployedTag != buildTag):
			add("lifecycle/"+name, true, fmt.Sprintf("build %s not deployed yet", buildTag), "warn")
		}
	}

	for _, rs := range routed {
		host := asString(rs.Svc["host"])
		zone := lv.ZoneOfHost(host)
		if zone == "" {
			add("cname/"+rs.Name, false, fmt.Sprintf("no zone covers %s", host), "")
			continue
		}
		if tokenErr != nil {
			add("cname/"+rs.Name, false, tokenErr.Error(), "")
			continue
		}
		recs, err := GetCnames(token, zone, host)
		want := lv.TunnelTarget()
		ok := false
		if err == nil && len(recs) > 0 {
			ok = asString(recs[0]["content"]) == want
		} else if err != nil {
			add("cname/"+rs.Name, false, err.Error(), "")
			continue
		}
		if ok {
			add("cname/"+rs.Name, true, "ok", "")
		} else {
			add("cname/"+rs.Name, false, fmt.Sprintf("want -> %s", want), "")
		}
	}

	return doctorFinish(checks, jsonMode)
}

func doctorFinish(checks []doctorCheck, jsonMode bool) int {
	if jsonMode {
		out, err := json.MarshalIndent(checks, "", "  ")
		if err != nil {
			return failf("json render failed: %v", err)
		}
		fmt.Println(string(out))
	} else {
		fmt.Println("== sos-lab doctor (read-only) ==")
		for _, c := range checks {
			mark := map[string]string{"ok": "ok  ", "warn": "WARN", "fail": "FAIL", "skip": "SKIP"}[c.Status]
			line := fmt.Sprintf("%s  %s", mark, c.Check)
			if c.Detail != "" && c.Status != "ok" {
				line += " — " + c.Detail
			} else if c.Status == "skip" {
				line += " — skipped"
			}
			fmt.Println(line)
		}
		failed, warned := 0, 0
		for _, c := range checks {
			switch c.Status {
			case "fail":
				failed++
			case "warn":
				warned++
			}
		}
		switch {
		case failed > 0:
			fmt.Printf("DOCTOR: %d problem(s) found\n", failed)
		case warned > 0:
			fmt.Printf("DOCTOR: all clear (%d warning(s))\n", warned)
		default:
			fmt.Println("DOCTOR: all clear")
		}
	}
	for _, c := range checks {
		if c.Status == "fail" {
			return 1
		}
	}
	return 0
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "\n"); i >= 0 {
		return s[:i]
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
