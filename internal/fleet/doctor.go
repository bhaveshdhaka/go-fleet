package fleet

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// failf prints the standard machine-contract error line and returns rc=1.
func failf(format string, args ...any) int {
	fmt.Fprintf(os.Stderr, "FLEET ERROR :: "+format+"\n", args...)
	return 1
}

// mustPaths resolves the repo root and enforces the registry-presence check
// scripts/fleet performs before every command that touches contracts.
func mustPaths() (Paths, int) {
	p, err := LoadPaths()
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return p, 1
	}
	if _, err := os.Stat(p.Registry); err != nil {
		fmt.Fprintf(os.Stderr, "FLEET ERROR :: registry missing at %s\n", p.Registry)
		return p, 1
	}
	return p, 0
}

// emitJSON prints one compact JSON document + newline (WO-17 --json
// surface on read verbs; additive to the text machine contracts). rc: 0.
func emitJSON(v any) int {
	raw, err := json.Marshal(v)
	if err != nil {
		return failf("json: %v", err)
	}
	fmt.Println(string(raw))
	return 0
}

// boolRc maps a boolean to the process exit code (0 pass, 1 fail).
func boolRc(ok bool) int {
	if ok {
		return 0
	}
	return 1
}

// cmdStatus mirrors do_status: one STATUS line per (selected) component,
// then the parseable summary. Missing kind/stage render as '?'.
// --json (WO-17): {"components":[{component,kind,stage}]} — additive.
func cmdStatus(args []string) int {
	p, rc := mustPaths()
	if rc != 0 {
		return rc
	}
	jsonMode := false
	sel := ""
	for _, a := range args {
		if a == "--json" {
			jsonMode = true
			continue
		}
		if sel == "" {
			sel = a
		}
	}
	regLines, err := readLines(p.Registry)
	if err != nil {
		return failf("registry unreadable at %s", p.Registry)
	}
	stateLines, _ := readLines(p.State)
	type jc struct {
		Component string `json:"component"`
		Kind      string `json:"kind"`
		Stage     string `json:"stage"`
	}
	var comps []jc
	n := 0
	for _, c := range registryNames(regLines) {
		if sel != "" && c != sel {
			continue
		}
		kind := fieldFor(regLines, c, "kind")
		stage := stateStage(stateLines, c)
		if kind == "" {
			kind = "?"
		}
		if stage == "" {
			stage = "?"
		}
		comps = append(comps, jc{c, kind, stage})
		n++
	}
	if jsonMode {
		return emitJSON(map[string]any{"components": comps})
	}
	for _, c := range comps {
		fmt.Printf("STATUS component=%s kind=%s stage=%s\n", c.Component, c.Kind, c.Stage)
	}
	fmt.Printf("STATUS SUMMARY components=%d\n", n)
	return 0
}

// filepathJoin mirrors bash "$FLEET_ROOT/$p" — plain concatenation, no
// path cleaning, so doctor compares exactly what the shell would.
func filepathJoin(root, rel string) string {
	return root + "/" + rel
}

// legalStage: the only stage values state may hold.
func legalStage(s string) bool {
	switch s {
	case "built", "dev", "stage", "prod":
		return true
	}
	return false
}

// cmdDoctor mirrors do_doctor: registry path fields on disk, legal stages,
// gate unit references resolvable, journal lines well-formed. Read-only.
// --json (WO-17): {"ok":bool,"issues":[...]} — additive.
func cmdDoctor(args []string) int {
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
	problems := 0
	var issueList []string
	problem := func(msg string) {
		if !jsonMode {
			fmt.Printf("DOCTOR ISSUE :: %s\n", msg)
		}
		issueList = append(issueList, msg)
		problems++
	}

	regLines, err := readLines(p.Registry)
	if err != nil {
		return failf("registry unreadable at %s", p.Registry)
	}
	stateLines, _ := readLines(p.State)
	cnt := 0
	for _, c := range registryNames(regLines) {
		for _, f := range []string{"path", "pipeline", "manifests"} {
			v := fieldFor(regLines, c, f)
			switch {
			case v == "":
				problem(fmt.Sprintf("registry: %s has empty %s", c, f))
			default:
				if _, err := os.Stat(filepathJoin(p.Root, v)); err != nil {
					problem(fmt.Sprintf("registry: %s %s '%s' does not exist on disk", c, f, v))
				}
			}
		}
		if stage := stateStage(stateLines, c); !legalStage(stage) {
			problem(fmt.Sprintf("state: component %s illegal stage '%s'", c, stage))
		}
		cnt++
	}

	gateLines, err := readLines(p.Gates)
	if err != nil {
		return failf("gates unreadable at %s", p.Gates)
	}
	seen := map[string]bool{}
	var units []string
	for _, u := range GateUnitsAll(gateLines) {
		if !seen[u] {
			seen[u] = true
			units = append(units, u)
		}
	}
	sortStrings(units)
	for _, u := range units {
		if st, err := os.Stat(filepathJoin(p.Tests, u)); err != nil || !st.IsDir() {
			problem(fmt.Sprintf("gates: references unknown unit '%s'", u))
		}
	}

	journalLines, _ := readLines(p.Journal)
	for _, ln := range journalLines {
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		if !journalLineRe.MatchString(ln) {
			preview := ln
			if len(preview) > 80 {
				preview = preview[:80]
			}
			problem(fmt.Sprintf("journal: malformed line: %s", preview))
			break
		}
	}

	if problems == 0 {
		if jsonMode {
			return emitJSON(map[string]any{"ok": true, "issues": issueList})
		}
		fmt.Printf("DOCTOR OK checked_components=%d issues=0\n", cnt)
		return 0
	}
	if jsonMode {
		return emitJSON(map[string]any{"ok": false, "issues": issueList})
	}
	fmt.Printf("DOCTOR FAIL checked_components=%d issues=%d\n", cnt, problems)
	return 1
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
