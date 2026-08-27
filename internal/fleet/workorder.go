package fleet

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Workorder front-matter schema v1 (WO-5). Markdown files are data the
// binary validates: the block between the first two `---` lines carries
// wo/title/status/plan and the pieces list with id/title/verify/integrated.
// Files without front-matter are legacy (schema 0); their status falls back
// to the prose `> **Status:**` header line.

type WOPiece struct {
	ID    string
	Title string
	// Verify is the piece's verify command; Integrated is the piece state:
	// "true" (landed), "false" (pending), or "deferred" (explicitly waived
	// by the owner, evidenced by a journal directive line). Only "false"
	// counts as unintegrated for predicate P5.
	Verify     string
	Integrated string
}

type Workorder struct {
	ID     string
	Title  string
	Status string
	Plan   string
	Path   string
	Pieces []WOPiece
	Schema int
}

func validWOStatus(s string) bool {
	switch s {
	case "OPEN", "IN_PROGRESS", "EXECUTED":
		return true
	}
	return false
}

// parseWorkorder reads one workorder file. Missing/invalid front-matter
// yields Schema 0 (legacy) with the prose-header status.
func parseWorkorder(path string) Workorder {
	w := Workorder{Path: path, Schema: 0}
	b, err := os.ReadFile(path)
	if err != nil {
		w.Status = "unreadable"
		return w
	}
	lines := strings.Split(string(b), "\n")
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		w.Schema = 1
		w.parseFrontMatter(lines[1:])
	}
	if w.ID == "" {
		w.ID = strings.TrimSuffix(filepath.Base(path), ".md")
	}
	if w.Status == "" || !validWOStatus(w.Status) {
		if !(w.Schema == 1 && w.Status != "") {
			w.Status = woStatusFromLines(lines)
		}
	}
	return w
}

func (w *Workorder) parseFrontMatter(lines []string) {
	inPieces := false
	cur := -1
	for _, ln := range lines {
		if strings.TrimSpace(ln) == "---" {
			return
		}
		switch {
		case ln == "pieces:":
			inPieces = true
		case inPieces && strings.HasPrefix(ln, "  - id:"):
			w.Pieces = append(w.Pieces, WOPiece{
				ID: strings.TrimSpace(strings.TrimPrefix(ln, "  - id:")),
			})
			cur = len(w.Pieces) - 1
		case inPieces && cur >= 0 && strings.HasPrefix(ln, "    "):
			kv := strings.SplitN(strings.TrimSpace(ln), ":", 2)
			if len(kv) != 2 {
				continue
			}
			val := strings.TrimSpace(kv[1])
			switch kv[0] {
			case "title":
				w.Pieces[cur].Title = val
			case "verify":
				w.Pieces[cur].Verify = val
			case "integrated":
				switch val {
				case "true", "false", "deferred":
					w.Pieces[cur].Integrated = val
				default:
					w.Pieces[cur].Integrated = "false"
				}
			}
		case !inPieces && strings.HasPrefix(ln, "  - ") || !inPieces && strings.HasPrefix(ln, "- "):
			// unsupported list at top level: ignore
		default:
			if inPieces {
				continue
			}
			kv := strings.SplitN(ln, ":", 2)
			if len(kv) != 2 || strings.HasPrefix(ln, " ") {
				continue
			}
			val := strings.TrimSpace(kv[1])
			switch strings.TrimSpace(kv[0]) {
			case "wo":
				w.ID = val
			case "title":
				w.Title = val
			case "status":
				w.Status = val
			case "plan":
				w.Plan = val
			}
		}
	}
}

// woStatusFromLines is the legacy prose-header parser (pre-WO-5 files).
func woStatusFromLines(lines []string) string {
	for _, ln := range lines {
		if m := woStatusRe.FindStringSubmatch(ln); m != nil {
			s := strings.TrimSpace(m[1])
			if i := strings.Index(s, "·"); i >= 0 {
				s = strings.TrimSpace(s[:i])
			}
			return s
		}
	}
	return "unknown"
}

// loadWorkorders returns all workorders sorted by ID.
func loadWorkorders(dir string) []Workorder {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Workorder
	for _, e := range entries {
		n := e.Name()
		if strings.HasPrefix(n, "WO-") && strings.HasSuffix(n, ".md") {
			out = append(out, parseWorkorder(filepath.Join(dir, n)))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (w Workorder) isActive() bool {
	return w.Status == "OPEN" || w.Status == "IN_PROGRESS"
}

func (w Workorder) unintegratedCount() int {
	n := 0
	for _, p := range w.Pieces {
		if p.Integrated == "false" {
			n++
		}
	}
	return n
}
