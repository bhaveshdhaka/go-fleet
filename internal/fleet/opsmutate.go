package fleet

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Ops mutation verbs (WO-8): build/deploy/rollback/dns/monitor/remove/
// verify — semantic mirrors of labctl/cli.py with lab-identical output
// contracts (BUILT / DEPLOYED / MONITOR OK / dns: / drift: / rolled back /
// removed / -> HTTP). All kubectl traffic goes through the explicit
// runner (rule 7); all mutations are dual-run with ./lab until parity
// passes. The ONE documented deviation: `remove` also deletes the
// service's state entries (./lab remove leaves doctor red forever).

// opsContext bundles what every mutation verb needs.
type opsContext struct {
	p       Paths
	site    Site
	lv      *LabView
	labRoot string
	stdout  io.Writer
}

func resolveOpsContext(p Paths, siteName string) (*opsContext, int) {
	sites, err := LoadSites(p)
	if err != nil {
		return nil, failf("sites registry unreadable: %v", err)
	}
	if len(sites) == 0 {
		return nil, failf("no sites registered in ops/SITES.yaml")
	}
	var site Site
	if siteName != "" {
		var found bool
		site, found = getSite(sites, siteName)
		if !found {
			return nil, failf("unknown site '%s'", siteName)
		}
	} else if len(sites) == 1 {
		site = sites[0]
	} else {
		return nil, failf("multiple sites registered; pass --site")
	}
	if site.Engine != "sos-lab" {
		return nil, failf("site '%s': engine '%s' not supported yet", site.Name, site.Engine)
	}
	if !validSiteAccess(site.Access) {
		return nil, failf("site '%s': invalid access mode '%s'", site.Name, site.Access)
	}
	lv, err := LoadLabView(site, p.Root)
	if err != nil {
		return nil, failf("%v", err)
	}
	return &opsContext{
		p:       p,
		site:    site,
		lv:      lv,
		labRoot: site.LabRootAbs(p.Root),
		stdout:  os.Stdout,
	}, 0
}

func (oc *opsContext) getService(name string) (map[string]any, error) {
	svc := oc.lv.LabServices()[name]
	if svc == nil {
		return nil, fmt.Errorf("service '%s' is not registered in %s",
			name, filepath.Join(oc.labRoot, "config", "registry.yaml"))
	}
	return svc, nil
}

// opError renders lab-style ERROR output (ops verbs speak the lab
// contract, not the FLEET ERROR contract — dual-run parity includes
// failure shapes).
func opError(err error) int {
	fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
	return 1
}

// --- ops build -----------------------------------------------------------

func opsBuild(oc *opsContext, args []string) int {
	name := ""
	allowDirty := false
	for _, a := range args {
		switch a {
		case "--allow-dirty":
			allowDirty = true
		default:
			if strings.HasPrefix(a, "-") {
				return opError(fmt.Errorf("unknown flag %q for ops build", a))
			}
			if name != "" {
				return opError(fmt.Errorf("ops build takes exactly one service"))
			}
			name = a
		}
	}
	if name == "" {
		return opError(fmt.Errorf("usage: fleet ops build <service> [--allow-dirty]"))
	}
	svc, err := oc.getService(name)
	if err != nil {
		return opError(err)
	}
	repo := asString(svc["repo"])
	if repo == "" {
		return opError(fmt.Errorf("service '%s' has no repo — it uses a prebuilt image", name))
	}
	path, err := labResolveRepoPath(oc.labRoot, repo)
	if err != nil {
		return opError(err)
	}
	gitSha := labGitSha(path)
	if pin := asString(svc["pin"]); pin != "" && gitSha != pin {
		head := "?"
		if gitSha != "" {
			head = gitSha[:8]
		}
		return opError(fmt.Errorf(
			"%s: repo HEAD %s != pinned %s — checkout the pinned sha, or update pin: in config/registry.yaml",
			name, head, pin[:8]))
	}
	if dirty := labGitDirty(path); dirty && !allowDirty {
		return opError(fmt.Errorf(
			"%s: %s has uncommitted changes — commit them first or rerun with --allow-dirty",
			name, path))
	}

	runner, cleanup, err := newKubectlRunner(oc.site, oc.p.Root)
	if err != nil {
		return opError(err)
	}
	defer cleanup()

	tag := labBuildTag(time.Now())
	compact := strings.ReplaceAll(tag, ".", "")
	srcCtx := "/workspace/" + filepath.Base(path)
	ns := oc.site.Namespace

	submit := func(jobName, ctxPath, dest string, extraArgs []string, dockerfile string) error {
		doc := renderLabKanikoJob(ns, jobName, ctxPath, dest, extraArgs, dockerfile)
		delArgs := []string{"-n", ns, "delete", "job", jobName, "--ignore-not-found"}
		if _, _, rc := runner.runTimeout(delArgs, "", labKubectlTimeout); rc != 0 {
			return kubectlErr(delArgs, "", "delete job failed", rc)
		}
		if err := applyLabDocs(runner, []any{doc}); err != nil {
			return err
		}
		fmt.Fprintf(oc.stdout, "== building %s (context %s)\n", dest, ctxPath)
		pod, err := labBuildPodForJob(runner, ns, jobName)
		if err != nil {
			return err
		}
		if err := labStream(runner, "-n", ns, "logs", "-f", pod); err != nil {
			if _, isExit := err.(*exec.ExitError); !isExit {
				return err
			}
		}
		if err := labWaitJob(runner, ns, jobName, "2400s"); err != nil {
			fmt.Fprintf(os.Stderr, "== build job failed: %v\n", err)
			fmt.Fprintf(os.Stderr, "%s\n", labDescribeTail(runner, ns, pod))
			return fmt.Errorf("image build failed: %s", dest)
		}
		return nil
	}

	var finalImage string
	if labOverlayDir(oc.labRoot, name) {
		baseDest := fmt.Sprintf("%s/%s-base:%s", LabRegistryHost, name, tag)
		if err := submit("build-"+name+"-"+compact, srcCtx, baseDest, nil, "Dockerfile"); err != nil {
			return opError(err)
		}
		fmt.Fprintf(oc.stdout, "-- base image ready: %s\n", baseDest)
		overlayCtx := "/workspace/" + filepath.Base(oc.labRoot) + "/images/" + name
		finalImage = fmt.Sprintf("%s/%s:%s", LabRegistryHost, name, tag)
		extra := []string{
			"--build-arg=BASE_IMAGE=" + baseDest,
			"--build-arg=KUBE_VERSION=" + LabKubeVersion,
			"--insecure-pull",
		}
		if err := submit("build-"+name+"-ovr-"+compact, overlayCtx, finalImage, extra, "Dockerfile"); err != nil {
			return opError(err)
		}
	} else {
		finalImage = fmt.Sprintf("%s/%s:%s", LabRegistryHost, name, tag)
		dockerfile := orStr(svc["dockerfile"], "Dockerfile")
		if err := submit("build-"+name+"-"+compact, srcCtx, finalImage, nil, dockerfile); err != nil {
			return opError(err)
		}
	}

	if err := recordLabBuild(oc.labRoot, name, tag, gitSha, time.Now()); err != nil {
		return opError(err)
	}
	sha := gitSha
	if sha == "" {
		sha = "unknown"
	}
	fmt.Fprintf(oc.stdout, "BUILT %s image=%s sha=%s\n", name, finalImage, sha)
	return 0
}

// --- ops deploy ----------------------------------------------------------

func opsDeploy(oc *opsContext, args []string) int {
	var names []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			return opError(fmt.Errorf("unknown flag %q for ops deploy", a))
		}
		names = append(names, a)
	}
	if len(names) == 0 {
		return opError(fmt.Errorf("usage: fleet ops deploy <service>..."))
	}
	// two-phase like cli.cmd_deploy: validate ALL services before any mutation
	type plan struct {
		name  string
		svc   map[string]any
		image string
	}
	var plans []plan
	for _, name := range names {
		svc, err := oc.getService(name)
		if err != nil {
			return opError(err)
		}
		if errTxt := oc.lv.CheckSecrets(name, svc, oc.p.Root); errTxt != "" {
			return opError(fmt.Errorf("%s: %s", name, errTxt))
		}
		image, err := labServiceImage(oc.lv, name, svc)
		if err != nil {
			return opError(err)
		}
		plans = append(plans, plan{name, svc, image})
	}

	runner, cleanup, err := newKubectlRunner(oc.site, oc.p.Root)
	if err != nil {
		return opError(err)
	}
	defer cleanup()

	for _, pl := range plans {
		if err := oc.deployOne(runner, pl.name, pl.svc, pl.image); err != nil {
			return opError(err)
		}
		fmt.Fprintf(oc.stdout, "-- %s done\n", pl.name)
	}
	if err := opsMonitorInner(oc, runner); err != nil {
		return opError(err)
	}
	joined := make([]string, len(plans))
	for i, pl := range plans {
		joined[i] = pl.name
	}
	fmt.Fprintf(oc.stdout, "DEPLOYED %s\n", strings.Join(joined, ", "))
	return 0
}

// deployOne mirrors cli._deploy_one.
func (oc *opsContext) deployOne(r *kubectlRunner, name string, svc map[string]any, image string) error {
	ns := asString(svc["namespace"])
	tag := labImageTag(image)
	fmt.Fprintf(oc.stdout, "== deploy %s (%s) ==\n", name, tag)

	envFile := filepath.Join(oc.labRoot, "secrets", name+".env")
	if _, err := os.Stat(envFile); err == nil {
		if err := labEnsureSecret(r, ns, name+"-env", envFile); err != nil {
			return err
		}
	}
	docs := renderLabService(oc.lv, oc.p.Root, name, svc, image)
	if err := applyLabDocs(r, docs); err != nil {
		return err
	}
	if err := labRolloutStatus(r, ns, name, "180s"); err != nil {
		return err
	}
	if host := asString(svc["host"]); host != "" {
		zone := oc.lv.ZoneOfHost(host)
		if zone == "" {
			return fmt.Errorf("no zone in registry covers %s", host)
		}
		token, err := LoadCloudflareToken(oc.labRoot)
		if err != nil {
			return err
		}
		_, msg, err := EnsureCname(token, zone, host, oc.lv.TunnelTarget(), true)
		if err != nil {
			return err
		}
		fmt.Fprintf(oc.stdout, "dns: %s\n", msg)
		cf := asMap(oc.lv.Registry["cloudflare"])
		if err := PutTunnelConfig(token, asString(cf["account_id"]), asString(cf["tunnel_id"]),
			renderLabTunnelIngress(oc.lv)); err != nil {
			return err
		}
		fmt.Fprintln(oc.stdout, "tunnel ingress synced")
	}
	if !asBool(svc["enabled"]) {
		if err := labSetServiceEnabled(oc.labRoot, name, true); err != nil {
			return err
		}
	}
	gitSha := stateEntry(oc.lv.Builds, name, "git_sha")
	if _, err := recordLabDeploy(oc.labRoot, name, tag, image, gitSha, time.Now()); err != nil {
		return err
	}
	fmt.Fprintf(oc.stdout, "deployed %s tag=%s\n", name, tag)
	return nil
}

// --- ops rollback --------------------------------------------------------

func opsRollback(oc *opsContext, args []string) int {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return opError(fmt.Errorf("usage: fleet ops rollback <service>"))
	}
	name := args[0]
	svc, err := oc.getService(name)
	if err != nil {
		return opError(err)
	}
	ns := asString(svc["namespace"])
	runner, cleanup, err := newKubectlRunner(oc.site, oc.p.Root)
	if err != nil {
		return opError(err)
	}
	defer cleanup()

	prev := labPreviousImage(runner, ns, name)
	if err := labRolloutUndo(runner, ns, name); err != nil {
		return opError(err)
	}
	if err := labRolloutStatus(runner, ns, name, "180s"); err != nil {
		return opError(err)
	}
	if prev != "" {
		entry, err := recordLabRollback(oc.labRoot, name, labImageTag(prev), prev, time.Now())
		if err != nil {
			return opError(err)
		}
		fmt.Fprintf(oc.stdout,
			"rolled back %s; state now records tag=%s image=%s (rolled_back=true)\n",
			name, entry["tag"], prev)
	} else {
		fmt.Fprintf(oc.stdout,
			"rolled back %s; WARNING: previous image unknown — run ./lab doctor to see live-vs-state drift\n", name)
	}
	return 0
}

// --- ops dns -------------------------------------------------------------

func opsDNS(oc *opsContext, args []string, jsonMode bool) int {
	apply := false
	for _, a := range args {
		if a == "--apply" {
			apply = true
			continue
		}
		return opError(fmt.Errorf("unknown flag %q for ops dns", a))
	}
	if jsonMode {
		return opError(fmt.Errorf("ops dns does not support --json"))
	}
	routed := oc.lv.RoutedServices()
	token, err := LoadCloudflareToken(oc.labRoot)
	if err != nil {
		return opError(err)
	}
	var drift []string
	for _, rs := range routed {
		host := asString(rs.Svc["host"])
		zone := oc.lv.ZoneOfHost(host)
		if zone == "" {
			return opError(fmt.Errorf("no zone covers %s", host))
		}
		status, msg, err := EnsureCname(token, zone, host, oc.lv.TunnelTarget(), apply)
		if err != nil {
			return opError(err)
		}
		if status != "ok" {
			drift = append(drift, msg)
		}
		prefix := "drift: "
		if apply {
			prefix = "dns: "
		}
		fmt.Fprintf(oc.stdout, "%s%s\n", prefix, msg)
	}
	cf := asMap(oc.lv.Registry["cloudflare"])
	liveIngress, err := TunnelIngress(token, asString(cf["account_id"]), asString(cf["tunnel_id"]))
	if err != nil {
		return opError(err)
	}
	wantSet := map[string]bool{}
	for _, rs := range routed {
		wantSet[asString(rs.Svc["host"])] = true
	}
	liveSet := map[string]bool{}
	for _, h := range liveIngress {
		liveSet[h] = true
	}
	if sameStringSet(wantSet, liveSet) {
		fmt.Fprintln(oc.stdout, "tunnel ingress matches registry")
	} else {
		missing := sortedMissing(wantSet, liveSet)
		extra := sortedMissing(liveSet, wantSet)
		diff := fmt.Sprintf("missing=%s extra=%s", fmtPythonList(missing), fmtPythonList(extra))
		if apply {
			if err := PutTunnelConfig(token, asString(cf["account_id"]), asString(cf["tunnel_id"]),
				renderLabTunnelIngress(oc.lv)); err != nil {
				return opError(err)
			}
			fmt.Fprintf(oc.stdout, "tunnel ingress synced %s\n", diff)
		} else {
			fmt.Fprintf(oc.stdout, "tunnel ingress DRIFT %s\n", diff)
		}
	}
	if len(drift) > 0 && !apply {
		fmt.Fprintln(oc.stdout, "(read-only report — rerun with --apply to reconcile)")
	}
	return 0
}

func sameStringSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func sortedMissing(want, have map[string]bool) []string {
	out := []string{}
	for k := range want {
		if !have[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// --- ops monitor ---------------------------------------------------------

func opsMonitor(oc *opsContext, args []string) int {
	for _, a := range args {
		return opError(fmt.Errorf("unknown flag %q for ops monitor", a))
	}
	runner, cleanup, err := newKubectlRunner(oc.site, oc.p.Root)
	if err != nil {
		return opError(err)
	}
	defer cleanup()
	if err := opsMonitorInner(oc, runner); err != nil {
		return opError(err)
	}
	return 0
}

// opsMonitorInner mirrors cli.cmd_monitor (shared with deploy).
func opsMonitorInner(oc *opsContext, r *kubectlRunner) error {
	slug, err := labDashboardSlug(oc.labRoot)
	if err != nil {
		return err
	}
	docs, err := renderLabMonitorDocs(oc.lv, oc.p.Root, slug)
	if err != nil {
		return err
	}
	if err := applyLabDocs(r, docs); err != nil {
		return err
	}
	gatusPath := filepath.Join(oc.labRoot, "templates", "gatus.yaml")
	out, errOut, rc := r.runTimeout([]string{"apply", "-f", gatusPath}, "", labKubectlTimeout)
	if rc != 0 {
		return kubectlErr([]string{"apply", "-f", gatusPath}, out, errOut, rc)
	}
	ns := oc.site.Namespace
	if err := labRolloutStatus(r, ns, "sos-dashboard", "180s"); err != nil {
		return err
	}
	if err := labRolloutRestart(r, ns, "gatus"); err != nil {
		return err
	}
	if err := labRolloutStatus(r, ns, "gatus", "180s"); err != nil {
		return err
	}
	n := len(oc.lv.RoutedServices())
	fmt.Fprintf(oc.stdout, "MONITOR OK: gatus + dashboard synced (%d endpoints)\n", n)
	return nil
}

// --- ops remove ----------------------------------------------------------

func opsRemove(oc *opsContext, args []string) int {
	name := ""
	deleteData, unregister := false, false
	for _, a := range args {
		switch a {
		case "--delete-data":
			deleteData = true
		case "--unregister":
			unregister = true
		default:
			if strings.HasPrefix(a, "-") {
				return opError(fmt.Errorf("unknown flag %q for ops remove", a))
			}
			if name != "" {
				return opError(fmt.Errorf("ops remove takes exactly one service"))
			}
			name = a
		}
	}
	if name == "" {
		return opError(fmt.Errorf("usage: fleet ops remove <service> [--delete-data] [--unregister]"))
	}
	svc, err := oc.getService(name)
	if err != nil {
		return opError(err)
	}
	ns := asString(svc["namespace"])

	runner, cleanup, err := newKubectlRunner(oc.site, oc.p.Root)
	if err != nil {
		return opError(err)
	}
	defer cleanup()

	if host := asString(svc["host"]); host != "" {
		if err := labSetServiceEnabled(oc.labRoot, name, false); err != nil {
			return opError(err)
		}
		// NOTE (journaled deviation #2): cli.cmd_remove PUTs the tunnel
		// config from the STALE in-memory registry (enabled still true),
		// leaving the removed host routed. fleet re-renders from the
		// updated file so the tunnel excludes it — the obvious intent,
		// required for "dns reports no drift" after teardown.
		fresh, err := LoadLabView(oc.site, oc.p.Root)
		if err != nil {
			return opError(err)
		}
		token, err := LoadCloudflareToken(oc.labRoot)
		if err != nil {
			return opError(err)
		}
		cf := asMap(fresh.Registry["cloudflare"])
		if err := PutTunnelConfig(token, asString(cf["account_id"]), asString(cf["tunnel_id"]),
			renderLabTunnelIngress(fresh)); err != nil {
			return opError(err)
		}
	}
	del := func(kind string) error {
		args := []string{"-n", ns, "delete", kind, name, "--ignore-not-found"}
		out, errOut, rc := runner.runTimeout(args, "", labKubectlTimeout)
		if rc != 0 {
			return kubectlErr(args, out, errOut, rc)
		}
		return nil
	}
	if err := del("deployment"); err != nil {
		return opError(err)
	}
	if err := del("service"); err != nil {
		return opError(err)
	}
	if deleteData {
		args := []string{"-n", ns, "delete", "pvc", name + "-data", "--ignore-not-found"}
		out, errOut, rc := runner.runTimeout(args, "", labKubectlTimeout)
		if rc != 0 {
			return opError(kubectlErr(args, out, errOut, rc))
		}
	}
	// fleet extension (journaled): clean state so doctor stays green
	if err := removeLabStateEntry(oc.labRoot, "deployed.json", name); err != nil {
		return opError(err)
	}
	if unregister {
		if err := labRemoveService(oc.labRoot, name); err != nil {
			return opError(err)
		}
		if err := removeLabStateEntry(oc.labRoot, "builds.json", name); err != nil {
			return opError(err)
		}
		fmt.Fprintf(oc.stdout, "removed %s from cluster and registry\n", name)
	} else {
		fmt.Fprintf(oc.stdout, "removed %s from cluster (registry entry kept, disabled)\n", name)
	}
	if err := opsMonitorInner(oc, runner); err != nil {
		return opError(err)
	}
	return 0
}

// --- ops verify ----------------------------------------------------------

func opsVerify(oc *opsContext, args []string) int {
	name := ""
	expect := 200
	expectNext := false
	for _, a := range args {
		switch {
		case a == "--expect":
			expectNext = true
		case expectNext:
			n, ok := atoiStrict(a)
			if !ok {
				return opError(fmt.Errorf("--expect requires an integer"))
			}
			expect = n
			expectNext = false
		case strings.HasPrefix(a, "--expect="):
			n, ok := atoiStrict(strings.TrimPrefix(a, "--expect="))
			if !ok {
				return opError(fmt.Errorf("--expect requires an integer"))
			}
			expect = n
		default:
			if strings.HasPrefix(a, "-") {
				return opError(fmt.Errorf("unknown flag %q for ops verify", a))
			}
			if name != "" {
				return opError(fmt.Errorf("ops verify takes exactly one service"))
			}
			name = a
		}
	}
	if name == "" {
		return opError(fmt.Errorf("usage: fleet ops verify <service> [--expect N]"))
	}
	svc, err := oc.getService(name)
	if err != nil {
		return opError(err)
	}
	host := asString(svc["host"])
	if host == "" {
		return opError(fmt.Errorf("service '%s' has no public host", name))
	}
	out, err := exec.Command("curl", "-sS", "-o", "/dev/null", "-w", "%{http_code}",
		"--max-time", "15", "https://"+host).Output()
	code := 0
	if err == nil {
		s := strings.TrimSpace(string(out))
		if n, ok := atoiStrict(s); ok {
			code = n
		}
	}
	ok := expect <= code && code < expect+100
	shown := "no-answer"
	if code != 0 {
		shown = fmt.Sprintf("%d", code)
	}
	verdict := "FAIL"
	if ok {
		verdict = "OK"
	}
	fmt.Fprintf(oc.stdout, "%s: https://%s -> HTTP %s %s\n", name, host, shown, verdict)
	if !ok {
		return 1
	}
	return 0
}

func atoiStrict(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
		n = n*10 + int(s[i]-'0')
	}
	return n, true
}
