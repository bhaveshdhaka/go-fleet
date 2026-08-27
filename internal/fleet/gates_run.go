package fleet

import (
	"os/exec"
	"regexp"
	"strings"
)

// runGateSuite re-executes the deterministic black-box test spine on the
// gate's units — the same `bash scripts/test.sh <units>` promote.sh runs.
// Stale green logs count for nothing (AGENTS.md rule 3).
func runGateSuite(p Paths, units []string) (string, error) {
	argv := []string{p.Root + "/scripts/test.sh"}
	argv = append(argv, units...)
	cmd := exec.Command("bash", argv...)
	cmd.Dir = p.Root
	out, err := cmd.CombinedOutput()
	return string(out), err
}

var gateSummaryRe = regexp.MustCompile(`^FLEET SUMMARY.*fail=([0-9]*).*$`)
var gateFailLineRe = regexp.MustCompile(`^\[FAIL\]`)

// lastGateFailCount mirrors sed -n 's/^FLEET SUMMARY.*fail=\([0-9]*\).*$/\1/p'
// piped through tail -1; "unknown" when no summary line was emitted.
func lastGateFailCount(out string) string {
	fails := "unknown"
	for _, ln := range strings.Split(out, "\n") {
		if m := gateSummaryRe.FindStringSubmatch(ln); m != nil {
			fails = m[1]
		}
	}
	return fails
}

// firstFails mirrors grep -E '^\[FAIL\]' | head -3 | tr '\n' ';' — each kept
// line is followed by a semicolon, exactly like tr renders it.
func firstFails(out string) string {
	var kept []string
	for _, ln := range strings.Split(out, "\n") {
		if gateFailLineRe.MatchString(ln) {
			kept = append(kept, ln+";")
			if len(kept) == 3 {
				break
			}
		}
	}
	return strings.Join(kept, "")
}
