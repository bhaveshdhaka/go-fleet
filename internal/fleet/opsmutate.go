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
	if !validSiteEngine(site.Engine) {
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
	path, err := labResolveRepoPath(oc.p.Root, repo)
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
	srcCtx, err := labWorkspaceHostPath(oc.p.Root, path)
	if err != nil {
		return opError(err)
	}
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
		overlayCtx, err := labWorkspaceHostPath(oc.p.Root, filepath.Join(oc.labRoot, "images", name))
		if err != nil {
			return opError(err)
		}
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

	envFile := filepath.Join(oc.site.secretsDir(oc.p.Root), name+".env")
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
		token, err := LoadCloudflareToken(oc.site.secretsDir(oc.p.Root))
		if err != nil {
			return err
		}
		_, msg, err := EnsureCname(token, zone, host, oc.lv.TunnelTarget(), true)
		if err != nil {
			return err
		}
		fmt.Fprintf(oc.stdout, "dns: %s\n", msg)
	}
	if !asBool(svc["enabled"]) {
		if err := labSetServiceEnabled(oc.labRoot, name, true); err != nil {
			return err
		}
	}
	// deviation #3 (WO-9): re-render the tunnel from the UPDATED registry
	// so a newly deployed host is routable immediately — lab PUTs before
	// the flip and relies on a separate `dns --apply` afterwards.
	fresh, err := LoadLabView(oc.site, oc.p.Root)
	if err != nil {
		return err
	}
	oc.lv = fresh
	if host := asString(svc["host"]); host != "" {
		token, err := LoadCloudflareToken(oc.site.secretsDir(oc.p.Root))
		if err != nil {
			return err
		}
		cf := asMap(fresh.Registry["cloudflare"])
		if err := PutTunnelConfig(token, asString(cf["account_id"]), asString(cf["tunnel_id"]),
			renderLabTunnelIngress(fresh)); err != nil {
			return err
		}
		fmt.Fprintln(oc.stdout, "tunnel ingress synced")
	}
	gitSha := stateEntry(fresh.Builds, name, "git_sha")
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
	token, err := LoadCloudflareToken(oc.site.secretsDir(oc.p.Root))
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

// opsMonitorInner mirrors cli.cmd_monitor (shared with deploy). Like
// labctl, it re-reads the registry and state fresh so flipped services
// and new deploys are reflected.
func opsMonitorInner(oc *opsContext, r *kubectlRunner) error {
	if fresh, err := LoadLabView(oc.site, oc.p.Root); err == nil {
		oc.lv = fresh
	}
	slug, err := labDashboardSlug(oc.site.secretsDir(oc.p.Root))
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
		token, err := LoadCloudflareToken(oc.site.secretsDir(oc.p.Root))
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

// --- ops register --------------------------------------------------------

// opsRegister mirrors labctl add_service: appends a canonical service
// block to the SITE registry (enabled: false by default) and validates by
// re-parse. WO-9: needed for the post-migration drill and WO-10
// onboarding. The projects.*.services display list is not maintained
// (nothing in the engine reads it).
// registerSpec carries every runtime-tuning knob ops register accepts
// (WO-16): the WO-8 basics plus the sos-lab-parity rich pod spec —
// storage/mounts/resources/probePath/runAsUser/serviceAccount/args.
type registerSpec struct {
	name, host, image, repo, dockerfile string
	port                                int
	ns                                  string
	secrets, envPairs                   []string
	probePath, runAsUser                string
	serviceAccount                      string
	argsList                            []string
	memReq, memLim, cpuReq, cpuLim      string
	storageSize, storageMount           string
	mountSubs, mountHosts               [][2]string // (sub|host, path)
}

func opsRegister(oc *opsContext, args []string) int {
	spec := registerSpec{ns: oc.site.Namespace}
	var secrets []string
	var envPairs []string
	expectVal := false
	prevFlag := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		if expectVal {
			expectVal = false
			switch prevFlag {
			case "--host":
				spec.host = a
			case "--port":
				n, ok := atoiStrict(a)
				if !ok || n <= 0 || n > 65535 {
					return opError(fmt.Errorf("--port must be an integer 1-65535"))
				}
				spec.port = n
			case "--namespace":
				spec.ns = a
			case "--image":
				spec.image = a
			case "--repo":
				spec.repo = a
			case "--dockerfile":
				spec.dockerfile = a
			case "--secret":
				secrets = append(secrets, a)
			case "--env":
				if !strings.Contains(a, "=") {
					return opError(fmt.Errorf("--env must be KEY=value"))
				}
				envPairs = append(envPairs, a)
			case "--probe-path":
				if !strings.HasPrefix(a, "/") {
					return opError(fmt.Errorf("--probe-path must start with /"))
				}
				spec.probePath = a
			case "--run-as-user":
				if _, ok := atoiStrict(a); !ok {
					return opError(fmt.Errorf("--run-as-user must be an integer uid"))
				}
				spec.runAsUser = a
			case "--service-account":
				spec.serviceAccount = a
			case "--args":
				spec.argsList = append(spec.argsList, a)
			case "--mem":
				req, lim, ok := splitReqLim(a)
				if !ok {
					return opError(fmt.Errorf("--mem must be REQ[:LIM] (e.g. 256Mi:2Gi)"))
				}
				spec.memReq, spec.memLim = req, lim
			case "--cpu":
				req, lim, ok := splitReqLim(a)
				if !ok {
					return opError(fmt.Errorf("--cpu must be REQ[:LIM] (e.g. 100m:1)"))
				}
				spec.cpuReq, spec.cpuLim = req, lim
			case "--storage":
				size, mount, ok := splitReqLim(a)
				if !ok || size == "" {
					return opError(fmt.Errorf("--storage must be SIZE[:MOUNT] (e.g. 5Gi:/data)"))
				}
				spec.storageSize, spec.storageMount = size, mount
			case "--mount-sub":
				sub, path, ok := splitReqLim(a)
				if !ok || sub == "" || !strings.HasPrefix(path, "/") {
					return opError(fmt.Errorf("--mount-sub must be NAME:/PATH (subPath:mountPath)"))
				}
				spec.mountSubs = append(spec.mountSubs, [2]string{sub, path})
			case "--mount-host":
				src, dst, ok := splitReqLim(a)
				if !ok || !strings.HasPrefix(src, "/") || !strings.HasPrefix(dst, "/") {
					return opError(fmt.Errorf("--mount-host must be /SRC:/DST (hostPath:mountPath)"))
				}
				spec.mountHosts = append(spec.mountHosts, [2]string{src, dst})
			}
			continue
		}
		switch a {
		case "--host", "--port", "--namespace", "--image", "--repo", "--dockerfile", "--secret", "--env",
			"--probe-path", "--run-as-user", "--service-account", "--args", "--mem", "--cpu", "--storage",
			"--mount-sub", "--mount-host":
			expectVal = true
			prevFlag = a
			continue
		}
		switch {
		case strings.HasPrefix(a, "--"):
			return opError(fmt.Errorf("unknown flag %q for ops register", a))
		default:
			if spec.name != "" {
				return opError(fmt.Errorf("ops register takes exactly one name"))
			}
			spec.name = a
		}
	}
	spec.secrets, spec.envPairs = secrets, envPairs
	if spec.name == "" || spec.port == 0 {
		return opError(fmt.Errorf(
			"usage: fleet ops register <name> --port <1-65535> [--host H] [--namespace NS] [--image IMG|--repo DIR] [--secret KEY]... [--env K=V]... [--probe-path /P] [--run-as-user UID] [--service-account SA] [--args A]... [--mem REQ[:LIM]] [--cpu REQ[:LIM]] [--storage SIZE[:MOUNT]] [--mount-sub NAME:/PATH]... [--mount-host /SRC:/DST]..."))
	}
	if !siteNameRe.MatchString(spec.name) {
		return opError(fmt.Errorf("service name must match %s", siteNameRe.String()))
	}
	if oc.lv.LabServices()[spec.name] != nil {
		return opError(fmt.Errorf("service '%s' is already registered", spec.name))
	}
	if spec.image == "" && spec.repo == "" {
		return opError(fmt.Errorf("service '%s': needs --image or --repo", spec.name))
	}
	if err := labRegistryAppendService(oc.labRoot, spec); err != nil {
		return opError(err)
	}
	fmt.Fprintf(oc.stdout, "registered %s\n", spec.name)
	fmt.Fprintf(oc.stdout, "next: fill %s if needed, then ./scripts/fleet ops deploy %s\n",
		filepath.Join(oc.site.secretsDir(oc.p.Root), spec.name+".env"), spec.name)
	return 0
}

// splitReqLim splits REQ[:LIM] on the FIRST colon (hostPath sources and
// mount paths never contain colons in this contract; storage SIZE never
// does either).
func splitReqLim(s string) (string, string, bool) {
	if s == "" {
		return "", "", false
	}
	i := strings.Index(s, ":")
	if i < 0 {
		return s, "", true
	}
	return s[:i], s[i+1:], true
}

// labRegistryAppendService appends a canonical service block at the end
// of the site registry (the services section is the last section of the
// file in our contracts), then re-validates.
func labRegistryAppendService(labRoot string, s registerSpec) error {
	path := filepath.Join(labRoot, "config", "registry.yaml")
	lines, err := readLines(path)
	if err != nil {
		return err
	}
	// refuse if the name is already present as a block
	if _, _, ok := labServiceBlock(lines, s.name); ok {
		return fmt.Errorf("service '%s' is already registered in %s", s.name, path)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "  %s:\n", s.name)
	fmt.Fprintf(&b, "    namespace: %s\n", s.ns)
	fmt.Fprintf(&b, "    port: %d\n", s.port)
	fmt.Fprintf(&b, "    enabled: false\n")
	if s.host != "" {
		fmt.Fprintf(&b, "    host: %s\n", s.host)
	}
	if s.image != "" {
		fmt.Fprintf(&b, "    image: %s\n", s.image)
	}
	if s.repo != "" {
		fmt.Fprintf(&b, "    repo: %s\n", s.repo)
	}
	if s.dockerfile != "" {
		fmt.Fprintf(&b, "    dockerfile: %s\n", s.dockerfile)
	}
	if s.probePath != "" {
		fmt.Fprintf(&b, "    probePath: %s\n", s.probePath)
	}
	if s.runAsUser != "" {
		fmt.Fprintf(&b, "    runAsUser: %s\n", s.runAsUser)
	}
	if s.serviceAccount != "" {
		fmt.Fprintf(&b, "    serviceAccount: %s\n", s.serviceAccount)
	}
	if len(s.argsList) > 0 {
		b.WriteString("    args:\n")
		for _, a := range s.argsList {
			fmt.Fprintf(&b, "    - %s\n", a)
		}
	}
	if len(s.secrets) > 0 {
		b.WriteString("    secrets:\n")
		for _, k := range s.secrets {
			fmt.Fprintf(&b, "    - %s\n", k)
		}
	}
	if len(s.envPairs) > 0 {
		b.WriteString("    env:\n")
		for _, kv := range s.envPairs {
			parts := strings.SplitN(kv, "=", 2)
			fmt.Fprintf(&b, "      %s: %s\n", parts[0], parts[1])
		}
	}
	if s.storageSize != "" {
		b.WriteString("    storage:\n")
		fmt.Fprintf(&b, "      size: %s\n", s.storageSize)
		if s.storageMount != "" {
			fmt.Fprintf(&b, "      mount: %s\n", s.storageMount)
		}
	}
	if len(s.mountSubs) > 0 || len(s.mountHosts) > 0 {
		b.WriteString("    mounts:\n")
		for _, m := range s.mountSubs {
			fmt.Fprintf(&b, "    - sub: %s\n", m[0])
			fmt.Fprintf(&b, "      path: %s\n", m[1])
		}
		for _, m := range s.mountHosts {
			fmt.Fprintf(&b, "    - host: %s\n", m[0])
			fmt.Fprintf(&b, "      path: %s\n", m[1])
		}
	}
	if s.memReq != "" || s.cpuReq != "" || s.memLim != "" || s.cpuLim != "" {
		b.WriteString("    resources:\n")
		if s.memReq != "" || s.cpuReq != "" {
			b.WriteString("      requests:\n")
			if s.memReq != "" {
				fmt.Fprintf(&b, "        memory: %s\n", s.memReq)
			}
			if s.cpuReq != "" {
				fmt.Fprintf(&b, "        cpu: %s\n", s.cpuReq)
			}
		}
		if s.memLim != "" || s.cpuLim != "" {
			b.WriteString("      limits:\n")
			if s.memLim != "" {
				fmt.Fprintf(&b, "        memory: %s\n", s.memLim)
			}
			if s.cpuLim != "" {
				fmt.Fprintf(&b, "        cpu: %s\n", s.cpuLim)
			}
		}
	}
	out := append(append([]string{}, lines...), strings.Split(strings.TrimSuffix(b.String(), "\n"), "\n")...)
	return labRegistryRewrite(labRoot, out, func(reg map[string]any) error {
		svc := asMap(asMap(reg["services"])[s.name])
		if svc == nil {
			return fmt.Errorf("service '%s' missing after registration", s.name)
		}
		if asInt(svc["port"]) != s.port {
			return fmt.Errorf("service '%s': port did not persist", s.name)
		}
		return nil
	})
}
