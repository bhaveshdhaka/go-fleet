// scripts/vm-tier/seed-server.go — static file server for the cloud-init
// seed directory (WO-20 close-out: the vm-tier carries no interpreted
// runtimes). Stdlib only; built into the gitignored .vm/ prefix by up.sh:
//
//	go build -trimpath -o .vm/seed-server scripts/vm-tier/seed-server.go
package main

import (
	"flag"
	"log"
	"net/http"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:18080", "listen address")
	dir := flag.String("dir", ".", "directory to serve")
	flag.Parse()
	log.SetFlags(0)
	log.Printf("seed-server serving %s on %s", *dir, *addr)
	log.Fatal(http.ListenAndServe(*addr, http.FileServer(http.Dir(*dir))))
}
