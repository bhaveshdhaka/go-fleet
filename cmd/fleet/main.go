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
  status [component] [--json]            read-only snapshot
  doctor [--json]                        read-only drift check
  next [--json]                          read-only guidance: next legal action
  check [--json]                         read-only predicates P1-P6 report
  registry-check                         alias of doctor for CI gates
  wo <list|show|new> [...]               workorder surface (schema v1)
  init [dir]                             scaffold the SDLC file skeleton
  onboard <component>                    register component (registry+pipeline+state)
  site list [--json]                     managed sites registry (read-only)
  site new <name> [--domain D ...]       scaffold a NEW fleet-managed site
                                         [--dry-run]           (MUTATES; WO-15)
  site tunnel create <site> --domain D   CF: create tunnel, store token,
                                         record ids+zone      (MUTATES; WO-15)
  site canary [--site S]                 register→build→deploy→verify→remove
                                         drill of a fresh install  (MUTATES)
  site init <name> --from <lab_root>     migrate an external site to
                                         fleet-managed data (MUTATES; WO-9)
  infra deploy [--site S]                registry+cloudflared+gatus+dashboard
                                         from site templates  (MUTATES; WO-15)
  ops <status|doctor> [--json]           site observation, sos-lab parity (read-only)
  ops register <name> [flags]            # register a service in the site registry
  ops <build|deploy|rollback|dns|monitor|remove|verify|register|update>
                                         site operations (mutations; WO-8
                                         dual-run PASSED, WO-9 fleet-managed)
  approve <component> <dev|prod> [who]   write approval file + journal
  promote <component> <to-stage> [...]   gated stage transition
  verify [units...]                      run the test corpus, journal the result

Machine contracts: text summary lines (STATUS SUMMARY / DOCTOR OK|FAIL /
NEXT action= / CHECK SUMMARY / PROMOTED / APPROVED / BUILT / DEPLOYED /
INFRA OK / CANARY PASS) are byte-stable; every read verb also accepts
--json (additive). Exit codes: 0 ok, 1 fail, 2 usage/policy refusal.
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
