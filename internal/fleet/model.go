package fleet

import (
	"bufio"
	"os"
	"strings"
)

// The parsers below mirror the awk semantics of scripts/fleet and
// ci/promote.sh line for line — same prefix matching on "  - name: <c>",
// same greedy field extraction, same gate-section rules. The registry,
// state and gates files ARE the machine contract; parsing must stay
// byte-faithful to the bash reference implementation.

func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		out = append(out, sc.Text())
	}
	return out, sc.Err()
}

// registryNames lists component names in file order (sed 's/^  - name: //p').
func registryNames(lines []string) []string {
	var out []string
	for _, ln := range lines {
		if strings.HasPrefix(ln, "  - name: ") {
			out = append(out, strings.TrimPrefix(ln, "  - name: "))
		}
	}
	return out
}

// hasComponentExact mirrors grep -Eq "^  - name: ${comp}[[:space:]]*$".
func hasComponentExact(lines []string, comp string) bool {
	for _, ln := range lines {
		if ln == "  - name: "+comp {
			return true
		}
	}
	return false
}

// fieldFor mirrors awk field_for: enter the block whose line STARTS WITH
// "  - name: <comp>" (pure prefix, like index(...)==1), take the first
// "    <key>:" line, strip greedily up to the LAST "key:" occurrence and
// one surrounding quote pair.
func fieldFor(lines []string, comp, key string) string {
	inblk := false
	for _, ln := range lines {
		if strings.HasPrefix(ln, "  - name: "+comp) {
			inblk = true
			continue
		}
		if inblk && strings.HasPrefix(ln, "  - name: ") {
			break
		}
		if !inblk {
			continue
		}
		if !strings.HasPrefix(ln, "    "+key+":") {
			continue
		}
		v := ln
		if i := strings.LastIndex(v, key+":"); i >= 0 {
			v = v[i+len(key)+1:]
		}
		v = strings.TrimLeft(v, " \t")
		if len(v) > 0 && (v[0] == '"' || v[0] == '\'') {
			v = v[1:]
		}
		if len(v) > 0 && (v[len(v)-1] == '"' || v[len(v)-1] == '\'') {
			v = v[:len(v)-1]
		}
		return v
	}
	return ""
}

// stateStage mirrors awk state_stage: first "stage:" line at any indent
// inside the component block, raw value (no quote stripping, like bash).
func stateStage(lines []string, comp string) string {
	inblk := false
	for _, ln := range lines {
		if strings.HasPrefix(ln, "  - name: "+comp) {
			inblk = true
			continue
		}
		if inblk && strings.HasPrefix(ln, "  - name: ") {
			break
		}
		if inblk && strings.HasPrefix(strings.TrimLeft(ln, " \t"), "stage:") {
			v := strings.TrimLeft(ln, " \t")
			v = strings.TrimPrefix(v, "stage:")
			return strings.TrimLeft(v, " \t")
		}
	}
	return ""
}

// promoteCurrentStage mirrors ci/promote.sh current_stage: first line in the
// block containing "stage:" anywhere, value after the colon, spaces trimmed.
func promoteCurrentStage(lines []string, comp string) string {
	inblk := false
	for _, ln := range lines {
		if strings.HasPrefix(ln, "  - name: "+comp) {
			inblk = true
			continue
		}
		if inblk && strings.HasPrefix(ln, "  - name: ") {
			break
		}
		if inblk {
			if i := strings.Index(ln, "stage:"); i >= 0 {
				return strings.TrimLeft(ln[i+len("stage:"):], " \t")
			}
		}
	}
	return ""
}

// isGateListItem: leading whitespace then "- " (awk /^[[:space:]]+-[[:space:]]/).
func isGateListItem(ln string) bool {
	t := strings.TrimLeft(ln, " \t")
	return strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "-\t")
}

// gateItemFirst mirrors ci/promote.sh: strip the FIRST leading "-<ws>" run
// then all whitespace (sub(/^[[:space:]]*-[[:space:]]*/, "") + gsub).
func gateItemFirst(ln string) string {
	t := strings.TrimLeft(ln, " \t")
	t = strings.TrimPrefix(t, "-")
	t = strings.TrimLeft(t, " \t")
	return squeezeSpace(t)
}

// gateItemGreedy mirrors the doctor gate_units awk: remove the GREEDY prefix
// ending in dash+whitespace then all whitespace (sub(/.*-[[:space:]]+/,"")).
func gateItemGreedy(ln string) string {
	t := ln
	if i := strings.LastIndex(t, "-\t"); i > strings.LastIndex(t, "- ") {
		t = t[i+2:]
	} else if i := strings.LastIndex(t, "- "); i >= 0 {
		t = t[i+2:]
	}
	return squeezeSpace(t)
}

func squeezeSpace(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r != ' ' && r != '\t' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isTopLevelKey(ln string) bool {
	if ln == "" {
		return false
	}
	c := ln[0]
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_'
}

// afterLast mirrors awk sub(/^.*<sep>/, ""): greedy up to the LAST sep.
func afterLast(s, sep string) string {
	if i := strings.LastIndex(s, sep); i >= 0 {
		return s[i+len(sep):]
	}
	return s
}

// GateUnitsAll mirrors the doctor gate_units awk: for every gate, units
// listed under requires_units (needs_approvals clears the section).
func GateUnitsAll(lines []string) []string {
	ingate, sec := false, false
	var out []string
	for _, ln := range lines {
		t := strings.TrimLeft(ln, " \t")
		if strings.HasPrefix(t, "- ") && strings.Contains(ln, "from:") {
			ingate = true
		}
		if ingate && strings.Contains(ln, "requires_units:") {
			sec = true
			continue
		}
		if ingate && strings.Contains(ln, "needs_approvals:") {
			sec = false
		}
		if sec && isGateListItem(ln) {
			out = append(out, gateItemGreedy(ln))
		}
		if isTopLevelKey(ln) {
			ingate, sec = false, false
		}
	}
	return out
}

// GateEdgeItem is one requires_units (Kind "U") or needs_approvals (Kind "A")
// entry for a single from→to edge, in ci/promote.sh gate_items format.
type GateEdgeItem struct {
	Kind string
	Name string
}

// GateEdgeItems mirrors the promote.sh gate_items awk for one edge.
func GateEdgeItems(lines []string, fromStage, toStage string) []GateEdgeItem {
	var out []GateEdgeItem
	f := ""
	want := false
	sec := ""
	for _, ln := range lines {
		t := strings.TrimLeft(ln, " \t")
		if strings.HasPrefix(t, "- ") && strings.Contains(ln, "from:") {
			v := squeezeSpace(ln)
			f = afterLast(v, "from:")
			want = false
			continue
		}
		if strings.HasPrefix(ln, "    to:") {
			tw := afterLast(squeezeSpace(ln), "to:")
			want = (f == fromStage && tw == toStage)
			sec = ""
			continue
		}
		if strings.HasPrefix(ln, "#") && !strings.Contains(ln, "!") {
			continue
		}
		if isTopLevelKey(ln) {
			sec = ""
			continue
		}
		if want {
			if strings.Contains(ln, "requires_units:") {
				sec = "U"
				continue
			}
			if strings.Contains(ln, "needs_approvals:") {
				sec = "A"
				continue
			}
			if sec != "" && isGateListItem(ln) {
				out = append(out, GateEdgeItem{Kind: sec, Name: gateItemFirst(ln)})
			}
		}
	}
	return out
}

// bashTrJoin reproduces `... | tr '\n' ' '`: items joined by single spaces
// with a trailing space when non-empty (byte-parity for dry-run lines).
func bashTrJoin(items []string) string {
	if len(items) == 0 {
		return ""
	}
	return strings.Join(items, " ") + " "
}
