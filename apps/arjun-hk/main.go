// arjun-hk — single-page menu site (dollarbucks menu), the first real
// customer built THROUGH the fleet product (WO-10) and deployed via
// fleet ops to hk-03-dev at arjun.hk.
//
// Contract (asserted by tests/C15a_arjun_site_contract over the running
// binary):
//   iOS    — viewport-fit=cover, apple-mobile-web-app-capable,
//            apple-mobile-web-app-status-bar-style translucent, theme-color,
//            safe-area-inset padding.
//   Safari — system font stack only (no webfonts, no external fetches),
//            -webkit-font-smoothing: antialiased.
//   retina — all artwork inline SVG (vector, resolution-independent) and
//            a min-device-pixel-ratio: 2 refinement.
// No JavaScript, no external requests, stdlib-only.
package main

import (
	_ "embed"
	"fmt"
	"net/http"
	"os"
)

//go:embed page.html
var page string

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, page)
	})
	addr := "127.0.0.1:8080"
	if p := os.Getenv("PORT"); p != "" {
		addr = "0.0.0.0:" + p
	}
	if err := http.ListenAndServe(addr, mux); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
