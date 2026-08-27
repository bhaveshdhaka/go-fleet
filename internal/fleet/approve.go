package fleet

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// cmdApprove mirrors do_approve: sign-off file + one journal line, idempotent.
func cmdApprove(args []string) int {
	p, rc := mustPaths()
	if rc != 0 {
		return rc
	}
	if len(args) < 2 || args[0] == "" || args[1] == "" {
		fmt.Fprintln(os.Stderr, "usage: fleet approve <component> <dev|prod> [approver]")
		return 1
	}
	comp, stg := args[0], args[1]
	who := "agent"
	if v := os.Getenv("FLEET_ACTOR"); v != "" {
		who = v
	}
	if len(args) > 2 && args[2] != "" {
		who = args[2]
	}

	regLines, err := readLines(p.Registry)
	if err != nil {
		return failf("registry unreadable at %s", p.Registry)
	}
	if !hasComponentExact(regLines, comp) {
		return failf("unknown component '%s'", comp)
	}
	if stg != "dev" && stg != "prod" {
		return failf("approvals apply to dev or prod only (got '%s')", stg)
	}

	path := ApprovalPath(p, stg, comp)
	if HasApproval(path) {
		fmt.Printf("ALREADY APPROVED component=%s stage=%s\n", comp, stg)
		return 0
	}
	ts := FleetTS(time.Now())
	if err := WriteApproval(path, who, ts); err != nil {
		return failf("cannot write approval file %s: %v", path, err)
	}
	if err := AppendJournal(p.Journal, fmt.Sprintf(
		"ts=%s event=approved component=%s stage=%s actor=%s", ts, comp, stg, who)); err != nil {
		return failf("cannot append journal: %v", err)
	}
	fmt.Printf("APPROVED component=%s stage=%s file=%s\n", comp, stg, strings.TrimPrefix(path, p.Root+"/"))
	return 0
}

// promoteUsage is the ci/promote.sh usage line, byte-identical.
const promoteUsage = "usage: promote <component> <to-stage> [--dry-run] [--skip-gates]"

// refuse prints the promote refusal contract line (stderr) and returns rc=1.
func refuse(msg string) int {
	fmt.Fprintf(os.Stderr, "PROMOTE REFUSED :: %s\n", msg)
	return 1
}

// cmdPromote mirrors ci/promote.sh end to end: legal hops, approval files
// non-empty, gate units RE-RUN green right now (unless --skip-gates), exact
// block state rewrite, one journal line, idempotent repeats.
func cmdPromote(args []string) int {
	p, rc := mustPaths()
	if rc != 0 {
		return rc
	}
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, promoteUsage)
		return 2
	}
	comp, toStage := args[0], args[1]
	dryRun, skipGates := false, false
	for _, a := range args[2:] {
		switch a {
		case "--dry-run":
			dryRun = true
		case "--skip-gates":
			skipGates = true
		default:
			fmt.Fprintf(os.Stderr, "unknown flag %s\n%s\n", a, promoteUsage)
			return 2
		}
	}

	regLines, err := readLines(p.Registry)
	if err != nil {
		return failf("registry unreadable at %s", p.Registry)
	}
	if !hasComponentExact(regLines, comp) {
		return refuse(fmt.Sprintf("unknown component '%s'", comp))
	}
	switch toStage {
	case "built", "dev", "stage", "prod":
	default:
		return refuse(fmt.Sprintf("illegal target stage '%s'", toStage))
	}

	stateLines, _ := readLines(p.State)
	fromStage := promoteCurrentStage(stateLines, comp)
	if fromStage == "" {
		return refuse(fmt.Sprintf("no current stage found for '%s'", comp))
	}
	if fromStage == toStage {
		if dryRun {
			fmt.Printf("[promote][dry-run] already at %s (no mutation)\n", toStage)
			return 0
		}
		fmt.Printf("ALREADY AT component=%s stage=%s\n", comp, toStage)
		return 0
	}
	legal := false
	switch fromStage + "->" + toStage {
	case "built->dev", "dev->stage", "stage->prod", "dev->prod":
		legal = true
	}
	if !legal {
		return refuse(fmt.Sprintf("illegal transition '%s' -> '%s'", fromStage, toStage))
	}

	gateLines, err := readLines(p.Gates)
	if err != nil {
		return failf("gates unreadable at %s", p.Gates)
	}
	items := GateEdgeItems(gateLines, fromStage, toStage)
	var approvalsNeeded, unitsNeeded []string
	for _, it := range items {
		if it.Kind == "A" {
			approvalsNeeded = append(approvalsNeeded, it.Name)
		} else {
			unitsNeeded = append(unitsNeeded, it.Name)
		}
	}

	missing := false
	for _, s := range approvalsNeeded {
		ap := ApprovalPath(p, s, comp)
		if !HasApproval(ap) {
			fmt.Fprintf(os.Stderr, "PROMOTE REFUSED :: missing approval file %s\n", ap)
			missing = true
		}
	}
	if missing {
		return 1
	}
	for _, u := range unitsNeeded {
		if st, err := os.Stat(filepathJoin(p.Tests, u)); err != nil || !st.IsDir() {
			return refuse(fmt.Sprintf("gate references unknown unit '%s'", u))
		}
	}

	if dryRun {
		fmt.Printf("[promote][dry-run] would enforce approvals: %s\n", bashTrJoin(approvalsNeeded))
		fmt.Printf("[promote][dry-run] would re-run units: %s\n", orNone(unitsNeeded))
		fmt.Printf("[promote][dry-run] would move %s: %s -> %s\n", comp, fromStage, toStage)
		return 0
	}

	if !skipGates && len(unitsNeeded) > 0 {
		out, err := runGateSuite(p, unitsNeeded)
		if err != nil {
			return refuse(fmt.Sprintf("gate suite failed to run: %v", err))
		}
		fails := lastGateFailCount(out)
		if fails != "0" {
			return refuse(fmt.Sprintf("gate suite failed (fail=%s). %s", fails, firstFails(out)))
		}
	}

	ts := FleetTS(time.Now())
	if err := RewriteStateStage(p.State, comp, toStage, ts); err != nil {
		return refuse(fmt.Sprintf("state render failed: %v", err))
	}
	actor := "agent"
	if v := os.Getenv("FLEET_ACTOR"); v != "" {
		actor = v
	}
	if err := AppendJournal(p.Journal, fmt.Sprintf(
		"ts=%s event=promoted component=%s from=%s to=%s actor=%s",
		ts, comp, fromStage, toStage, actor)); err != nil {
		return failf("cannot append journal: %v", err)
	}
	fmt.Printf("PROMOTED component=%s from=%s to=%s at=%s\n", comp, fromStage, toStage, ts)
	return 0
}

// orNone mirrors bash ${unit_list[*]:-(none)}.
func orNone(items []string) string {
	if len(items) == 0 {
		return "(none)"
	}
	return strings.Join(items, " ")
}
