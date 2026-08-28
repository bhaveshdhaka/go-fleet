// apps/canary — the site canary (WO-15): a minimal stdlib-only Go httpd
// that reports which build is being served. Driven by
// `fleet site canary` through the FULL ship path (register → build →
// deploy → public verify → remove) to prove an install end to end.
package main

import (
	"fmt"
	"net/http"
	"os"
)

var buildTag = "dev"

func main() {
	if t := os.Getenv("CANARY_BUILD_TAG"); t != "" {
		buildTag = t
	}
	port := "8080"
	if p := os.Getenv("PORT"); p != "" {
		port = p
	}
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "fleet canary OK build=%s\n", buildTag)
	})
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	})
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		panic(err)
	}
}
