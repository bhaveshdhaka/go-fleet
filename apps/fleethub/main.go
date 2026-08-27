// fleethub — fleet lab dashboard + approval writer.
//
// Reads deployment INTENT (ops/PROJECTS.yaml), runtime TRUTH
// (ops/state/deployments.yaml) and the audit journal straight from disk.
// The only mutation it performs is writing approval sign-off files —
// exactly what ./fleet approve does — so every state change remains a
// plain, git-diffable file. stdlib-only; hermetic builds enforced by
// block 03. Bound to 127.0.0.1 by default (agents/humans on the box).
package main

import (
	"bufio"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	pageTmpl = `<!doctype html>
<html><head><title>fleet hub</title>
<style>
 body{font-family:ui-monospace,monospace;margin:2rem;background:#111;color:#ddd}
 table{border-collapse:collapse}td,th{border:1px solid #444;padding:.35rem .8rem;text-align:left}
 h1{font-weight:400}.ok{color:#7dcf85}.warn{color:#e6b450}
 form{display:inline-block;margin-top:1rem}
 input,button{font-family:inherit;padding:.3rem .5rem;background:#1c1c1c;border:1px solid #555;color:#ddd}
 pre{background:#161616;border:1px solid #333;padding:.5rem 1rem;overflow:auto}
</style></head><body>
<h1>fleet hub — component status</h1>
<table>
<tr><th>component</th><th>kind</th><th>stage</th><th>dev approved?</th><th>prod approved?</th></tr>
{{range .Components}}
<tr><td>{{.Name}}</td><td>{{.Kind}}</td><td>{{.Stage}}</td>
<td>{{if .DevApproved}}<span class="ok">yes</span>{{else}}<span class="warn">no</span>{{end}}</td>
<td>{{if .ProdApproved}}<span class="ok">yes</span>{{else}}<span class="warn">no</span>{{end}}</td></tr>
{{end}}
</table>
<form method="POST" action="/approve">
 <input name="component" placeholder="component" required>
 <select name="stage"><option value="dev">dev</option><option value="prod">prod</option></select>
 <input name="approver" placeholder="approver id" required>
 <button type="submit">approve</button>
</form>
<h2>journal tail</h2>
<pre>{{.JournalTail}}</pre>
<p><a href="/api/projects" style="color:#7dcf85">/api/projects</a> ·
   <a href="/healthz" style="color:#7dcf85">/healthz</a></p>
</body></html>`
)

type Component struct {
	Name, Kind, Stage          string
	DevApproved, ProdApproved  bool
}

type Hub struct {
	root string // repo root containing ops/ and lifecycle/
}

func (h *Hub) readLines(p string) []string {
	f, err := os.Open(p)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		out = append(out, sc.Text())
	}
	return out
}

func (h *Hub) components() ([]Component, error) {
	var names []string
	inReg := false
	for _, l := range h.readLines(filepath.Join(h.root, "ops", "PROJECTS.yaml")) {
		switch {
		case strings.HasPrefix(l, "  - name: "):
			names = append(names, strings.TrimSpace(strings.TrimPrefix(l, "  - name: ")))
			inReg = true
		case inReg && strings.HasPrefix(l, "registry_version"):
			inReg = false
		}
	}

	stage := func(name string) string {
		cur := ""
		found := false
		for _, l := range h.readLines(filepath.Join(h.root, "ops", "state", "deployments.yaml")) {
			if strings.HasPrefix(l, "  - name: ") {
				n := strings.TrimSpace(strings.TrimPrefix(l, "  - name: "))
				found = n == name
				continue
			}
			if found && strings.HasPrefix(strings.TrimLeft(l, " "), "stage:") {
				v := strings.TrimSpace(strings.SplitN(l, ":", 2)[1])
				cur = v
				found = false
			}
		}
		return cur
	}
	kind := func(name string) string {
		found := false
		for _, l := range h.readLines(filepath.Join(h.root, "ops", "PROJECTS.yaml")) {
			if strings.HasPrefix(l, "  - name: ") {
				found = strings.TrimSpace(strings.TrimPrefix(l, "  - name: ")) == name
				continue
			}
			if found && strings.HasPrefix(strings.TrimLeft(l, " "), "kind:") {
				v := strings.TrimSpace(strings.SplitN(l, ":", 2)[1])
				return v
			}
		}
		return "?"
	}
	approved := func(stage, name string) bool {
		p := filepath.Join(h.root, "lifecycle", "approvals", stage, name+".approved")
		fi, err := os.Stat(p)
		return err == nil && fi.Size() > 0
	}

	out := make([]Component, 0, len(names))
	for _, n := range names {
		out = append(out, Component{
			Name:         n,
			Kind:         kind(n),
			Stage:        stage(n),
			DevApproved:  approved("dev", n),
			ProdApproved: approved("prod", n),
		})
	}
	return out, nil
}

func (h *Hub) legalApprovalTarget(name, stage string) bool {
	found := false
	for _, l := range h.readLines(filepath.Join(h.root, "ops", "PROJECTS.yaml")) {
		if strings.HasPrefix(l, "  - name: ") &&
			strings.TrimSpace(strings.TrimPrefix(l, "  - name: ")) == name {
			found = true
		}
	}
	if !found {
		return false
	}
	return stage == "dev" || stage == "prod"
}

func (h *Hub) handleHome(w http.ResponseWriter, _ *http.Request) {
	comps, err := h.components()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	jr := h.readLines(filepath.Join(h.root, "lifecycle", "journal", "events.log"))
	start := len(jr) - 10
	if start < 0 {
		start = 0
	}
	tail := strings.Join(filterEvents(jr[start:]), "\n")
	data := struct {
		Components  []Component
		JournalTail string
	}{comps, tail}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := template.Must(template.New("p").Parse(pageTmpl)).Execute(w, data); err != nil {
		http.Error(w, err.Error(), 500)
	}
}

func filterEvents(lines []string) []string {
	var ev []string
	for _, l := range lines {
		if strings.HasPrefix(l, "ts=") {
			ev = append(ev, l)
		}
	}
	return ev
}

func writeJSONResponse(w http.ResponseWriter, code int, payload string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	fmt.Fprintln(w, payload)
}

func (h *Hub) handleAPI(w http.ResponseWriter, _ *http.Request) {
	comps, err := h.components()
	if err != nil {
		writeJSONResponse(w, 500, `{"error":"internal"}`)
		return
	}
	var b strings.Builder
	b.WriteString(`{"components":[`)
	for i, c := range comps {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b,
			`{"name":%q,"kind":%q,"stage":%q,"dev_approved":%t,"prod_approved":%t}`,
			c.Name, c.Kind, c.Stage, c.DevApproved, c.ProdApproved)
	}
	b.WriteString(`]}`)
	writeJSONResponse(w, 200, b.String())
}

func (h *Hub) handleApprove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONResponse(w, 405, `{"error":"method not allowed"}`)
		return
	}
	name := strings.TrimSpace(r.FormValue("component"))
	stage := strings.TrimSpace(r.FormValue("stage"))
	actor := strings.TrimSpace(r.FormValue("approver"))
	if actor == "" {
		actor = "fleethub-web"
	}
	ts := time.Now().UTC().Format(time.RFC3339)

	if !h.legalApprovalTarget(name, stage) {
		writeJSONResponse(w, 400,
			`{"error":"unknown component or illegal approval stage","allowed_stages":["dev","prod"]}`)
		return
	}

	dir := filepath.Join(h.root, "lifecycle", "approvals", stage)
	file := filepath.Join(dir, name+".approved")
	if fi, err := os.Stat(file); err == nil && fi.Size() > 0 {
		writeJSONResponse(w, 200, fmt.Sprintf(
			`{"result":"already","component":%q,"stage":%q}`, name, stage))
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeJSONResponse(w, 500, `{"error":"cannot create approvals dir"}`)
		return
	}
	payload := fmt.Sprintf("approved_by=%s\nts=%s\nsource=fleethub-http\n", actor, ts)
	if err := os.WriteFile(file, []byte(payload), 0o644); err != nil {
		writeJSONResponse(w, 500, `{"error":"write failed"}`)
		return
	}
	line := fmt.Sprintf("ts=%s event=approved component=%s stage=%s actor=%s source=fleethub-http\n",
		ts, name, stage, actor)
	f, err := os.OpenFile(filepath.Join(h.root, "lifecycle", "journal", "events.log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err == nil {
		_, _ = f.WriteString(line)
		_ = f.Close()
	}
	writeJSONResponse(w, 201, fmt.Sprintf(
		`{"result":"approved","component":%q,"stage":%q}`, name, stage))
}

func discoverRoot(start string) string {
	dir := start
	for i := 0; i < 16; i++ {
		if fi, err := os.Stat(filepath.Join(dir, "ops", "PROJECTS.yaml")); err == nil && fi.Mode().IsRegular() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func main() {
	addr := envOr("FLEETHUB_ADDR", "127.0.0.1:8099")
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Printf("fleethub %s\n", version)
		return
	}
	root := os.Getenv("FLEET_ROOT")
	if root == "" {
		exe, _ := os.Executable()
		root = discoverRoot(filepath.Dir(exe))
	}
	if root == "" || !dirExists(filepath.Join(root, "lifecycle", "journal")) {
		fmt.Fprintln(os.Stderr, "fleethub: no ops/ or lifecycle tree found near binary (set FLEET_ROOT)")
		os.Exit(2)
	}
	h := &Hub{root: root}
	mux := http.NewServeMux()
	mux.HandleFunc("/", h.handleHome)
	mux.HandleFunc("/api/projects", h.handleAPI)
	mux.HandleFunc("/approve", h.handleApprove)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprintln(w, "ok") })

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	fmt.Printf("fleethub listening on %s root=%s version=%s\n", addr, root, version)
	if err := srv.ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, "fleethub:", err)
		os.Exit(1)
	}
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

var version = "dev"
