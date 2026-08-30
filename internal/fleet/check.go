package fleet

import (
	"regexp"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Predicates P1-P7 (WO-5; P7 WO-22): file-state checks over the SDLC process. They
// REPORT authoring drift with exact fix commands (enforcement-policy
// decision in PLAN.md); the promote gates remain the only hard blocks.

type CheckResult struct {
	Predicate string
	State     string // PASS | FAIL | SKIP
	Detail    string
	Fix       string
}

// journalHasVerifyLine reports whether the journal contains a verify
// comment tagging the given workorder id.
func journalHasVerifyLine(journalLines []string, woID string) bool {
	tag := "wo=" + woID
	for _, ln := range journalLines {
		if strings.HasPrefix(ln, "#") && strings.Contains(ln, tag) &&
			strings.Contains(ln, "verify") {
			return true
		}
	}
	return false
}

// gitDiffEventsRemoved counts removed or modified ts= event lines between
// HEAD and the working tree (append-only discipline). notGit=true when the
// repo has no .git (scratch copies).
func gitDiffEventsRemoved(root string) (removed int, notGit bool, err error) {
	gitDir := filepath.Join(root, ".git")
	if _, e := os.Stat(gitDir); e != nil {
		return 0, true, nil
	}
	cmd := exec.Command("git", "diff", "HEAD", "--name-only")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return 0, false, err
	}
	touched := false
	for _, ln := range strings.Split(string(out), "\n") {
		if strings.HasSuffix(ln, "lifecycle/journal/events.log") {
			touched = true
			break
		}
	}
	if !touched {
		return 0, false, nil
	}
	cmd = exec.Command("git", "diff", "HEAD", "--", "lifecycle/journal/events.log")
	cmd.Dir = root
	out, err = cmd.Output()
	if err != nil {
		return 0, false, err
	}
	for _, ln := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(ln, "-") && !strings.HasPrefix(ln, "---") &&
			strings.HasPrefix(strings.TrimPrefix(ln, "-"), "ts=") {
			removed++
		}
	}
	return removed, false, nil
}

// gitTreeDirty reports whether the working tree has any tracked change or
// untracked non-ignored file.
func gitTreeDirty(root string) (dirty bool, notGit bool, err error) {
	if _, e := os.Stat(filepath.Join(root, ".git")); e != nil {
		return false, true, nil
	}
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return false, false, err
	}
	for _, ln := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(ln) != "" {
			return true, false, nil
		}
	}
	return false, false, nil
}

// RunPredicates evaluates P1-P7 against the repo rooted at p.
func RunPredicates(p Paths) []CheckResult {
	var res []CheckResult
	wos := loadWorkorders(filepath.Join(p.Root, "workorders"))
	journalLines, _ := readLines(p.Journal)

	dirty, notGit, err := gitTreeDirty(p.Root)
	active := 0
	for _, w := range wos {
		if w.isActive() {
			active++
		}
	}
	switch {
	case notGit:
		res = append(res, CheckResult{"P1", "SKIP", "not a git repo", ""})
	case err != nil:
		res = append(res, CheckResult{"P1", "SKIP", fmt.Sprintf("git failed: %v", err), ""})
	case dirty && active == 0:
		res = append(res, CheckResult{"P1", "FAIL",
			"dirty tree without an OPEN/IN_PROGRESS workorder",
			"./scripts/fleet wo new WO-<n> \"<title>\""})
	default:
		res = append(res, CheckResult{"P1", "PASS",
			fmt.Sprintf("dirty=%v active_workorders=%d", dirty, active), ""})
	}

	// P2 missing plan link (front-matter workorders only)
	bad := []string{}
	for _, w := range wos {
		if w.Schema == 1 && (w.Plan == "" || !strings.Contains(w.Plan, "PLAN.md")) {
			bad = append(bad, w.ID)
		}
	}
	if len(bad) > 0 {
		res = append(res, CheckResult{"P2", "FAIL",
			"missing plan link: " + strings.Join(bad, ","),
			"set plan: PLAN.md in workorders/<id>.md front-matter"})
	} else {
		res = append(res, CheckResult{"P2", "PASS", "all workorders link PLAN.md", ""})
	}

	// P3 missing decomposition (front-matter workorders only)
	bad = []string{}
	for _, w := range wos {
		if w.Schema == 1 && len(w.Pieces) == 0 {
			bad = append(bad, w.ID)
		}
	}
	if len(bad) > 0 {
		res = append(res, CheckResult{"P3", "FAIL",
			"no pieces: " + strings.Join(bad, ","),
			"add a pieces: list to workorders/<id>.md front-matter"})
	} else {
		res = append(res, CheckResult{"P3", "PASS", "all workorders decomposed", ""})
	}

	// P4 unjournaled verify (IN_PROGRESS workorders only)
	bad = []string{}
	for _, w := range wos {
		if w.Status == "IN_PROGRESS" && !journalHasVerifyLine(journalLines, w.ID) {
			bad = append(bad, w.ID)
		}
	}
	if len(bad) > 0 {
		res = append(res, CheckResult{"P4", "FAIL",
			"unjournaled verify: " + strings.Join(bad, ","),
			"./scripts/fleet verify [units...] (FLEET_WO=<id> tags the journal line)"})
	} else {
		res = append(res, CheckResult{"P4", "PASS", "active workorders have journaled verifies", ""})
	}

	// P5 unintegrated pieces (EXECUTED front-matter workorders only)
	bad = []string{}
	for _, w := range wos {
		if w.Status == "EXECUTED" && w.Schema == 1 && w.unintegratedCount() > 0 {
			bad = append(bad, fmt.Sprintf("%s(%d)", w.ID, w.unintegratedCount()))
		}
	}
	if len(bad) > 0 {
		res = append(res, CheckResult{"P5", "FAIL",
			"unintegrated pieces: " + strings.Join(bad, ","),
			"finish and set integrated: true in workorders/<id>.md"})
	} else {
		res = append(res, CheckResult{"P5", "PASS", "executed workorders fully integrated", ""})
	}

	// P6 journal tamper
	removed, notGit, err := gitDiffEventsRemoved(p.Root)
	switch {
	case notGit:
		res = append(res, CheckResult{"P6", "SKIP", "not a git repo", ""})
	case err != nil:
		res = append(res, CheckResult{"P6", "SKIP", fmt.Sprintf("git failed: %v", err), ""})
	case removed > 0:
		res = append(res, CheckResult{"P6", "FAIL",
			fmt.Sprintf("journal history rewritten: %d ts= line(s) removed/modified", removed),
			"restore lifecycle/journal/events.log from git history; the journal is append-only"})
	default:
		res = append(res, CheckResult{"P6", "PASS", "journal append-only discipline holds", ""})
	}

	// P7 journey coverage (WO-22, AGENTS rule 9 — permanent tier): active
	// workorders must reference at least one journey unit in a piece
	// verify, or declare journeys_exempt (docs/fixture-only changes).
	bad7 := []string{}
	for _, w := range wos {
		if !w.isActive() || w.JourneysExempt {
			continue
		}
		if !woReferencesJourney(p, w) {
			bad7 = append(bad7, w.ID)
		}
	}
	if len(bad7) > 0 {
		res = append(res, CheckResult{"P7", "FAIL",
			"active workorders without journey coverage: " + strings.Join(bad7, ","),
			"add a journey unit to a piece verify (see lifecycle/JOURNEYS.md) or set journeys_exempt: true with a justification"})
	} else {
		res = append(res, CheckResult{"P7", "PASS", "active workorders carry journey coverage or are exempt", ""})
	}
	return res
}

// woReferencesJourney reports whether any piece verify of w names a
// corpus unit whose run.sh sources the shared journey library (rule 9).
func woReferencesJourney(p Paths, w Workorder) bool {
	unitRe := regexp.MustCompile(`C[0-9][0-9A-Za-z_]*`)
	for _, piece := range w.Pieces {
		for _, u := range unitRe.FindAllString(piece.Verify, -1) {
			b, err := os.ReadFile(filepath.Join(p.Tests, u, "run.sh"))
			if err == nil && strings.Contains(string(b), "tests/lib/journey.sh") {
				return true
			}
		}
	}
	return false
}

// cmdCheck evaluates P1-P7 and prints the machine-parse report.
func cmdCheck(args []string) int {
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
	results := RunPredicates(p)
	if jsonMode {
		type cr struct {
			Predicate string `json:"predicate"`
			State     string `json:"state"`
			Detail    string `json:"detail"`
			Fix       string `json:"fix,omitempty"`
		}
		out := make([]cr, 0, len(results))
		pass, fail, skip := 0, 0, 0
		for _, r := range results {
			out = append(out, cr{r.Predicate, r.State, r.Detail, r.Fix})
			switch r.State {
			case "PASS":
				pass++
			case "FAIL":
				fail++
			default:
				skip++
			}
		}
		if err := emitJSON(map[string]any{
			"predicates": out,
			"total":      len(results), "pass": pass, "fail": fail, "skip": skip,
		}); err != 0 {
			return err
		}
		return boolRc(fail == 0)
	}
	pass, fail, skip := 0, 0, 0
	for _, r := range results {
		fmt.Printf("CHECK %s %s detail=%s\n", r.Predicate, r.State, r.Detail)
		if r.Fix != "" && r.State == "FAIL" {
			fmt.Printf("CHECK %s FIX %s\n", r.Predicate, r.Fix)
		}
		switch r.State {
		case "PASS":
			pass++
		case "FAIL":
			fail++
		default:
			skip++
		}
	}
	fmt.Printf("CHECK SUMMARY total=%d pass=%d fail=%d skip=%d\n",
		len(results), pass, fail, skip)
	if fail > 0 {
		return 1
	}
	return 0
}
