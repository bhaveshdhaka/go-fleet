// cmd/fleet — THE fleet control-plane binary (WO-4 Go core).
//
// Replaces bash scripts/fleet per PLAN.md WO-4: same file contracts, same
// machine-parse summary lines, byte-identical behavior. Built hermetically
// by ci/build-fleet.sh (pinned toolchain, GOPROXY=off, trimpath, no VCS
// stamping) — repeat builds must be byte-identical (C9a).
package main

import (
	"fmt"
	"os"

	"github.com/bhaveshdhaka/go-fleet/internal/fleet"
)

var version = "dev"

const helpText = `fleet <command>
  status [component]                     read-only snapshot
  doctor                                 read-only drift check
  registry-check                         alias of doctor for CI gates
  approve <component> <dev|prod> [who]   write approval file + journal
  promote <component> <to-stage> [...]   gated stage transition
  init [dir]                             scaffold the SDLC file skeleton
  onboard <component>                    register component (registry+pipeline+state)
  next                                   suggest the next legal action
  wo <list|show|new> [...]               workorder surface (minimal)
  verify [units...]                      run the test corpus, journal the result
`

func main() {
	args := os.Args[1:]
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Print(helpText)
		return
	}
	if args[0] == "version" {
		fmt.Printf("fleet %s\n", version)
		return
	}
	os.Exit(fleet.Run(args[0], args[1:]))
}
