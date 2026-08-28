package fleet

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// --- init --------------------------------------------------------------

// cmdInit scaffolds the SDLC file skeleton from embedded templates into the
// target dir. Idempotent: an already-initialized dir is reported, never
// mutated (zero-delta contract, asserted by C9c).
func cmdInit(args []string) int {
	target := "."
	if len(args) > 0 {
		target = args[0]
	}
	root, err := filepath.Abs(target)
	if err != nil {
		return failf("bad target dir: %v", err)
	}
	reg := filepath.Join(root, "ops", "PROJECTS.yaml")
	if _, err := os.Stat(reg); err == nil {
		fmt.Printf("INIT ALREADY dir=%s\n", root)
		return 0
	}
	files := 0
	scaffold := map[string]string{
		"ops/PROJECTS.yaml":            "registry.yaml.tmpl",
		"ops/ENVIRONMENTS.yaml":        "environments.yaml.tmpl",
		"ops/state/deployments.yaml":   "deployments.yaml.tmpl",
		"lifecycle/gates.yaml":         "gates.yaml.tmpl",
		"lifecycle/STAGES.md":          "stages.md.tmpl",
		"lifecycle/journal/events.log": "journal.log.tmpl",
		".fleet.yaml":                  "fleet.yaml.tmpl",
	}
	for _, rel := range []string{
		"ops/PROJECTS.yaml",
		"ops/ENVIRONMENTS.yaml",
		"ops/state/deployments.yaml",
		"lifecycle/gates.yaml",
		"lifecycle/STAGES.md",
		"lifecycle/journal/events.log",
		".fleet.yaml",
	} {
		if err := writeSeed(filepath.Join(root, rel), scaffold[rel]); err != nil {
			return failf("cannot write %s: %v", rel, err)
		}
		files++
	}
	for _, d := range []string{
		"lifecycle/approvals/dev", "lifecycle/approvals/prod", "ci/pipelines",
	} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			return failf("cannot create %s: %v", d, err)
		}
	}
	fmt.Printf("INIT OK dir=%s files=%d\n", root, files)
	fmt.Println("NEXT action=./scripts/fleet onboard <component>")
	return 0
}

// --- onboard -----------------------------------------------------------

// cmdOnboard registers a component: one registry entry + one pipeline file
// + one state entry (AGENTS.md rule 4), then points at fleet doctor.
func cmdOnboard(args []string) int {
	p, rc := mustPaths()
	if rc != 0 {
		return rc
	}
	var name string
	opts := map[string]string{
		"kind": "service", "path": "", "entrypoint": "main.go",
		"manifests": "infra/k8s", "description": "onboarded by fleet",
	}
	for _, a := range args {
		if strings.HasPrefix(a, "--") {
			kv := strings.SplitN(strings.TrimPrefix(a, "--"), "=", 2)
			if len(kv) == 2 {
				opts[kv[0]] = kv[1]
				continue
			}
			return failf("bad flag '%s' (expected --key=value)", a)
		}
		if name == "" {
			name = a
		} else {
			return failf("unexpected argument '%s'", a)
		}
	}
	if name == "" {
		fmt.Fprintln(os.Stderr, "usage: fleet onboard <name> [--kind=cli|service] [--path=apps/name] [--entrypoint=main.go] [--manifests=infra/k8s] [--description=...]")
		return 1
	}
	if !validComponentName(name) {
		return failf("invalid component name '%s' (use [a-z0-9._-])", name)
	}
	if opts["kind"] != "cli" && opts["kind"] != "service" {
		return failf("kind must be cli or service (got '%s')", opts["kind"])
	}
	if opts["path"] == "" {
		opts["path"] = "apps/" + name
	}

	regLines, err := readLines(p.Registry)
	if err != nil {
		return failf("registry unreadable at %s", p.Registry)
	}
	if hasComponentExact(regLines, name) {
		return failf("component '%s' already registered", name)
	}

	pipeline := "ci/pipelines/" + name + ".yaml"
	// Render the pipeline file FIRST: template errors must surface before
	// any contract file is mutated (onboard is not atomic, so order the
	// failure modes from harmless to structural).
	if err := renderTemplate(filepath.Join(p.Root, pipeline), "pipeline.yaml.tmpl", templateData(map[string]string{
		"Name": name, "Manifests": opts["manifests"],
	})); err != nil {
		return failf("cannot write pipeline file: %v", err)
	}
	entry := fmt.Sprintf("\n  - name: %s\n    kind: %s\n    path: %s\n    language: go"+
		"\n    entrypoint: %s\n    pipeline: %s\n    manifests: %s"+
		"\n    description: %s\n    enabled: true\n",
		name, opts["kind"], opts["path"], opts["entrypoint"], pipeline,
		opts["manifests"], opts["description"])
	f, err := os.OpenFile(p.Registry, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return failf("cannot append registry: %v", err)
	}
	if _, err := f.WriteString(entry); err != nil {
		f.Close()
		return failf("cannot append registry: %v", err)
	}
	f.Close()

	stateEntry := fmt.Sprintf("\n  - name: %s\n    stage: built"+
		"\n    last_promoted_at: \"\"\n    note: onboarded by fleet\n", name)
	sf, err := os.OpenFile(p.State, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return failf("cannot append state: %v", err)
	}
	if _, err := sf.WriteString(stateEntry); err != nil {
		sf.Close()
		return failf("cannot append state: %v", err)
	}
	sf.Close()

	fmt.Printf("ONBOARDED component=%s registry=+1 pipeline=%s state=+1\n", name, pipeline)
	fmt.Println("NEXT action=./scripts/fleet doctor")
	return 0
}

// --- next --------------------------------------------------------------

// cmdNext is the guidance engine (WO-5): predicates P1-P6 in order first —
// the first FAIL becomes the suggestion with its exact fix command — then
// the doctor precheck, then the first pending lifecycle hop in registry
// order.
func cmdNext(args []string) int {
	p, rc := mustPaths()
	if rc != 0 {
		return rc
	}
	jsonMode := false
	for _, a := range args {
		if a == "--json" {
			jsonMode = true
		}
	}
	nextEmit := func(action, reason string) int {
		if jsonMode {
			return emitJSON(map[string]any{"action": action, "reason": reason})
		}
		fmt.Printf("NEXT action=%s\n", action)
		fmt.Printf("NEXT reason=%s\n", reason)
		return 0
	}
	nextPred := func(r CheckResult) int {
		if jsonMode {
			return emitJSON(map[string]any{"action": r.Fix, "reason": r.Detail, "predicate": r.Predicate})
		}
		fmt.Printf("NEXT predicate=%s\n", r.Predicate)
		fmt.Printf("NEXT action=%s\n", r.Fix)
		fmt.Printf("NEXT reason=%s\n", r.Detail)
		return 0
	}
	for _, r := range RunPredicates(p) {
		if r.State == "FAIL" {
			return nextPred(r)
		}
	}

	regLines, err := readLines(p.Registry)
	if err != nil {
		return failf("registry unreadable at %s", p.Registry)
	}
	stateLines, _ := readLines(p.State)
	gateLines, err := readLines(p.Gates)
	if err != nil {
		return failf("gates unreadable at %s", p.Gates)
	}
	journalLines, _ := readLines(p.Journal)

	for _, ln := range journalLines {
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		if !journalLineRe.MatchString(ln) {
			fmt.Println("NEXT action=./scripts/fleet doctor")
			fmt.Println("NEXT reason=journal has a malformed line")
			return 0
		}
	}
	probe := argsProbe{p: p, reg: regLines, state: stateLines, gates: gateLines}
	if issue := probe.doctorIssue(); issue != "" {
		return nextEmit("./scripts/fleet doctor", issue)
	}
	for _, c := range registryNames(regLines) {
		stage := stateStage(stateLines, c)
		switch stage {
		case "built":
			return nextEmit(fmt.Sprintf("./scripts/fleet promote %s dev", c), fmt.Sprintf("%s is built; built->dev needs no approval", c))
		case "dev":
			if !HasApproval(ApprovalPath(p, "dev", c)) {
				return nextEmit(fmt.Sprintf("./scripts/fleet approve %s dev", c), "dev->stage gate needs the dev approval")
			}
			return nextEmit(fmt.Sprintf("./scripts/fleet promote %s stage", c), "dev approval present; hop is legal")
		case "stage":
			if !HasApproval(ApprovalPath(p, "prod", c)) {
				return nextEmit(fmt.Sprintf("./scripts/fleet approve %s prod", c), "stage->prod gate needs the prod approval")
			}
			return nextEmit(fmt.Sprintf("./scripts/fleet promote %s prod", c), "prod approval present; hop is legal")
		}
	}
	// ops ladder (WO-16): a component at prod that maps to a site service
	// still owes build → deploy. Same registry order; lifecycle hops always
	// win. A component with no site-registry entry (e.g. kind: cli) is not
	// shippable and is skipped silently.
	if opsAdvice := nextOpsStep(p, regLines, stateLines); opsAdvice != nil {
		return nextEmit(opsAdvice.action, opsAdvice.reason)
	}
	return nextEmit("none", "no pending transitions in registry order")
}

// nextOpsStep returns the first owed ops action for a prod-stage
// component, or "" when the whole registry is shipped.
func nextOpsStep(p Paths, regLines, stateLines []string) *nextAction {
	sites, err := LoadSites(p)
	if err != nil || len(sites) == 0 {
		return nil
	}
	site := sites[0] // single-site is the default deployment; multi-site passes --site explicitly
	lv, err := LoadLabView(site, p.Root)
	if err != nil {
		return nil
	}
	for _, c := range registryNames(regLines) {
		if stateStage(stateLines, c) != "prod" {
			continue
		}
		svc, ok := lv.LabServices()[c]
		if !ok {
			continue
		}
		_ = svc
		builtTag := asString(asMap(lv.Builds[c])["tag"])
		deployedTag := asString(asMap(lv.Deployed[c])["tag"])
		if builtTag == "" {
			return &nextAction{
				action: fmt.Sprintf("./scripts/fleet ops build --site %s %s", site.Name, c),
				reason: fmt.Sprintf("%s is at prod and registered on site %s but has no build yet", c, site.Name),
			}
		}
		if deployedTag != builtTag {
			dep := deployedTag
			if dep == "" {
				dep = "nothing"
			}
			return &nextAction{
				action: fmt.Sprintf("./scripts/fleet ops deploy --site %s %s", site.Name, c),
				reason: fmt.Sprintf("%s build %s is not what is deployed (%s)", c, builtTag, dep),
			}
		}
	}
	return nil
}

type nextAction struct {
	action string
	reason string
}

// argsProbe bundles the contract lines doctor needs for one quiet pass.
type argsProbe struct {
	p     Paths
	reg   []string
	state []string
	gates []string
}

// doctorIssue returns the first doctor problem, or "" when clean.
func (a argsProbe) doctorIssue() string {
	for _, c := range registryNames(a.reg) {
		for _, f := range []string{"path", "pipeline", "manifests"} {
			v := fieldFor(a.reg, c, f)
			if v == "" {
				return fmt.Sprintf("registry: %s has empty %s", c, f)
			}
			if _, err := os.Stat(filepathJoin(a.p.Root, v)); err != nil {
				return fmt.Sprintf("registry: %s %s '%s' does not exist on disk", c, f, v)
			}
		}
		if stage := stateStage(a.state, c); !legalStage(stage) {
			return fmt.Sprintf("state: component %s illegal stage '%s'", c, stage)
		}
	}
	seen := map[string]bool{}
	var units []string
	for _, u := range GateUnitsAll(a.gates) {
		if !seen[u] {
			seen[u] = true
			units = append(units, u)
		}
	}
	sort.Strings(units)
	for _, u := range units {
		if st, err := os.Stat(filepathJoin(a.p.Tests, u)); err != nil || !st.IsDir() {
			return fmt.Sprintf("gates: references unknown unit '%s'", u)
		}
	}
	return ""
}

// --- wo ----------------------------------------------------------------

var woStatusRe = regexp.MustCompile(`^>\s+\*\*Status:\*\*\s*(.*)$`)

func cmdWo(args []string) int {
	p, rc := mustPaths()
	if rc != 0 {
		return rc
	}
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	wodir := filepath.Join(p.Root, "workorders")
	switch sub {
	case "list", "":
		wos := loadWorkorders(wodir)
		fmt.Printf("WO LIST count=%d\n", len(wos))
		for _, w := range wos {
			schema := "legacy"
			if w.Schema == 1 {
				schema = "v1"
			}
			fmt.Printf("WO id=%s status=%s schema=%s file=workorders/%s.md\n",
				w.ID, w.Status, schema, w.ID)
		}
		return 0
	case "show":
		if len(args) < 2 {
			return failf("usage: fleet wo show <WO-id>")
		}
		f := filepath.Join(wodir, args[1]+".md")
		b, err := os.ReadFile(f)
		if err != nil {
			return failf("no such workorder '%s'", args[1])
		}
		fmt.Printf("WO id=%s file=%s\n", args[1], strings.TrimPrefix(f, p.Root+"/"))
		os.Stdout.Write(b)
		return 0
	case "new":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: fleet wo new <WO-id> <title words...>")
			return 1
		}
		id := args[1]
		if !validWorkorderID(id) {
			return failf("invalid workorder id '%s' (use [A-Za-z0-9._-])", id)
		}
		f := filepath.Join(wodir, id+".md")
		if _, err := os.Stat(f); err == nil {
			return failf("workorder file already exists: %s", strings.TrimPrefix(f, p.Root+"/"))
		}
		title := strings.Join(args[2:], " ")
		if err := renderTemplate(f, "workorder.md.tmpl", templateData(map[string]string{"ID": id, "Title": title})); err != nil {
			return failf("cannot write workorder: %v", err)
		}
		fmt.Printf("WO NEW id=%s file=%s\n", id, strings.TrimPrefix(f, p.Root+"/"))
		return 0
	default:
		return failf("unknown wo subcommand '%s' (list|show|new)", sub)
	}
}

// --- verify --------------------------------------------------------------

var summaryRe = regexp.MustCompile(`^FLEET SUMMARY\s+units_run=(\d+) pass=(\d+) fail=(\d+) skip=(\d+)`)

// cmdVerify runs the deterministic black-box corpus (optionally restricted
// to the named units) and journals the measured result as a `# verify`
// line — the machine-readable seed of WO-5's P4 (unjournaled verify).
func cmdVerify(args []string) int {
	p, rc := mustPaths()
	if rc != 0 {
		return rc
	}
	argv := []string{p.Root + "/scripts/test.sh"}
	argv = append(argv, args...)
	out, err := exec.Command("bash", argv...).CombinedOutput()
	units, pass, fail, skip := "0", "0", "unknown", "0"
	for _, ln := range strings.Split(string(out), "\n") {
		if m := summaryRe.FindStringSubmatch(ln); m != nil {
			units, pass, fail, skip = m[1], m[2], m[3], m[4]
		}
	}
	result := "FAIL"
	if fail == "0" {
		result = "OK"
	}
	// Every verify attempt is journaled, including crashed ones — an
	// unjournaled verify is exactly the hole WO-5's P4 predicate closes.
	tag := ""
	if wo := os.Getenv("FLEET_WO"); wo != "" && validWorkorderID(wo) {
		tag = " wo=" + wo
	}
	if err := AppendJournal(p.Journal, fmt.Sprintf(
		"# verify ts=%s%s units=%s pass=%s fail=%s skip=%s result=%s",
		FleetTS(time.Now()), tag, units, pass, fail, skip, result)); err != nil {
		return failf("cannot append journal: %v", err)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s", out)
		return failf("corpus could not run: %v", err)
	}
	if fail != "0" {
		for _, ln := range strings.Split(string(out), "\n") {
			if strings.HasPrefix(ln, "[FAIL]") {
				fmt.Fprintln(os.Stderr, ln)
			}
		}
	}
	fmt.Printf("VERIFY units=%s pass=%s fail=%s skip=%s result=%s\n", units, pass, fail, skip, result)
	if result == "OK" {
		return 0
	}
	return 1
}
