package fleet

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// journalLineRe is the machine contract for journal history lines, verbatim
// from scripts/fleet doctor: ts, event vocabulary, component token.
var journalLineRe = regexp.MustCompile(`^ts=[^ ]+ event=(approved|promoted|rejected) component=[A-Za-z0-9._-]+`)

// FleetTS is the only timestamp format in the journal and state files,
// matching `date -u +%Y-%m-%dT%H:%M:%SZ`.
func FleetTS(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05Z")
}

// AppendJournal appends one full line to the append-only audit journal.
// History lines are never rewritten (STAGES.md rule 4).
func AppendJournal(path, line string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "%s\n", line)
	return err
}

// RewriteStateStage mirrors the promote.sh awk block rewrite byte-for-byte:
// inside the named component's block only, `stage:` becomes the new stage
// and `last_promoted_at:` becomes the quoted timestamp; every other line,
// comment and blank line passes through untouched. Atomic via temp+rename.
func RewriteStateStage(path, comp, newStage, ts string) error {
	lines, err := readLines(path)
	if err != nil {
		return err
	}
	var b strings.Builder
	inblk := false
	for _, ln := range lines {
		if strings.HasPrefix(ln, "  - name: "+comp) {
			inblk = true
			b.WriteString(ln)
			b.WriteByte('\n')
			continue
		}
		if inblk && strings.HasPrefix(ln, "  - name: ") {
			inblk = false
		}
		switch {
		case inblk && strings.HasPrefix(strings.TrimLeft(ln, " \t"), "stage:"):
			b.WriteString(replaceAfterFirstColon(ln, ": "+newStage))
		case inblk && strings.HasPrefix(strings.TrimLeft(ln, " \t"), "last_promoted_at:"):
			b.WriteString(replaceAfterFirstColon(ln, `: "`+ts+`"`))
		default:
			b.WriteString(ln)
		}
		b.WriteByte('\n')
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".deployments-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.WriteString(b.String()); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, path)
}

// replaceAfterFirstColon mirrors awk sub(/:.*/, "<repl>"): everything from
// the FIRST colon onward is replaced, indentation is preserved.
func replaceAfterFirstColon(ln, repl string) string {
	i := strings.Index(ln, ":")
	if i < 0 {
		return ln
	}
	return ln[:i] + repl
}

// ApprovalPath is the canonical approval file location.
func ApprovalPath(p Paths, stage, comp string) string {
	return filepath.Join(p.Approvals, stage, comp+".approved")
}

// HasApproval mirrors [[ -s file ]]: exists and non-empty.
func HasApproval(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Size() > 0
}

// WriteApproval writes the sign-off file exactly as scripts/fleet does:
//
//	approved_by=<who>
//	ts=<FleetTS>
func WriteApproval(path, who, ts string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte("approved_by="+who+"\nts="+ts+"\n"), 0o644)
}
