// apps/dashboard-render — the Go port of dashboard-render.py (WO-20
// piece 4): fleet table + principles -> /webroot/index.html, 60s loop.
// Stdlib-only, CGO_ENABLED=0 static binary. Kills the last python in
// the live estate.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func esc(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

func loadJSON(dir, name string) map[string]any {
	raw, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return map[string]any{}
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return map[string]any{}
	}
	return m
}

func fetchHealth(api string) map[string]string {
	out := map[string]string{}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(api)
	if err != nil {
		return out
	}
	defer resp.Body.Close()
	var body any
	if json.NewDecoder(resp.Body).Decode(&body) != nil {
		return out
	}
	eps, _ := body.([]any)
	if m, ok := body.(map[string]any); ok {
		eps, _ = m["results"].([]any)
	}
	for _, e := range eps {
		ep, ok := e.(map[string]any)
		if !ok {
			continue
		}
		name, _ := ep["name"].(string)
		up := false
		if checks, ok := ep["results"].([]any); ok && len(checks) > 0 {
			last, _ := checks[len(checks)-1].(map[string]any)
			up, _ = last["success"].(bool)
		} else {
			st, _ := ep["status"].(string)
			up = strings.EqualFold(st, "up") || strings.EqualFold(st, "healthy")
		}
		if up {
			out[name] = "up"
		} else {
			out[name] = "down"
		}
	}
	return out
}

func render() string {
	stateDir := getenv("STATE_DIR", "/state")
	api := getenv("GATUS_API", "http://gatus.sos-lab.svc.cluster.local:8080/api/v1/endpoints/statuses")
	state := loadJSON(stateDir, "state.json")
	hosts := loadJSON(stateDir, "hosts.json")
	principles := loadJSON(stateDir, "principles.json")
	health := fetchHealth(api)

	names := map[string]bool{}
	for k := range state {
		names[k] = true
	}
	for k := range health {
		names[k] = true
	}
	sorted := make([]string, 0, len(names))
	for k := range names {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)

	var rows strings.Builder
	for _, svc := range sorted {
		info, _ := state[svc].(map[string]any)
		tag := "-"
		if v, ok := info["tag"].(string); ok && v != "" {
			tag = v
		}
		dot := `<span class="dot down"></span>down`
		if health[svc] == "up" {
			dot = `<span class="dot up"></span>up`
		}
		host, _ := hosts[svc].(string)
		url := "-"
		if host != "" {
			url = fmt.Sprintf(`<a href="https://%s">%s</a>`, esc(host), esc(host))
		}
		fmt.Fprintf(&rows, "<tr><td>%s</td><td><code>%s</code></td><td>%s</td><td>%s</td></tr>",
			esc(svc), esc(tag), dot, url)
	}
	var prin strings.Builder
	pk := make([]string, 0, len(principles))
	for k := range principles {
		pk = append(pk, k)
	}
	sort.Strings(pk)
	for _, k := range pk {
		v, _ := principles[k].(string)
		fmt.Fprintf(&prin, "<li><strong>%s</strong> — %s</li>", esc(strings.ReplaceAll(k, "_", " ")), esc(v))
	}
	ts := time.Now().UTC().Format("2006-01-02 15:04:05")
	return fmt.Sprintf(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>sos-lab fleet</title>
<style>
body{background:#0d1117;color:#c9d1d9;font-family:system-ui,-apple-system,sans-serif;margin:2rem;max-width:960px}
h1{color:#58a6ff;font-size:1.5rem} h2{color:#8b949e;font-size:1.1rem;margin-top:2rem}
table{border-collapse:collapse;width:100%%}
th,td{padding:.5rem .75rem;border-bottom:1px solid #21262d;text-align:left;font-size:.95rem}
th{color:#8b949e;text-transform:uppercase;font-size:.75rem;letter-spacing:.05em}
.dot{display:inline-block;width:10px;height:10px;border-radius:50%%;margin-right:.4rem;vertical-align:middle}
.up{background:#3fb950}.down{background:#f85149}
a{color:#58a6ff;text-decoration:none} code{color:#79c0ff;background:#161b22;padding:.1rem .3rem;border-radius:4px}
li{margin:.4rem 0} strong{color:#e6edf3} footer{margin-top:2rem;color:#484f58;font-size:.8rem}
</style>
</head>
<body>
<h1>sos-lab fleet</h1>
<table>
<tr><th>service</th><th>tag</th><th>health</th><th>url</th></tr>
%s</table>
<h2>System Principles</h2>
<ul>
%s</ul>
<footer>rendered %s UTC &middot; sos-dashboard</footer>
</body>
</html>
`, rows.String(), prin.String(), ts)
}

func main() {
	out := getenv("OUT", "/webroot/index.html")
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	go func() {
		for {
			if html := render(); html != "" {
				tmp := out + ".tmp"
				if os.WriteFile(tmp, []byte(html), 0o644) == nil {
					os.Rename(tmp, out)
				}
			}
			time.Sleep(60 * time.Second)
		}
	}()
	http.ListenAndServe(":"+getenv("PORT", "9090"), mux)
}
