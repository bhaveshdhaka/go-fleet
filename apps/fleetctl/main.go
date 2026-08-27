// fleetctl — fleet lab pipeline sample binary.
//
// Deliberately dependency-free so the hermetic test tier can build it with
// GOPROXY=off. The version stamp is injected at build time by
// scripts/blocks/03-pipeline.sh via -ldflags -X main.version=<pin>.
package main

import (
	"fmt"
	"os"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Printf("fleetctl %s\n", version)
		return
	}
	fmt.Fprintf(os.Stderr, "usage: fleetctl version\n")
	os.Exit(2)
}
