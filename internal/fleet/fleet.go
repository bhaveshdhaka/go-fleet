// Package fleet implements the WO-4 Go core: file-contract readers/writers
// and the control-plane commands behind cmd/fleet. Root discovery and all
// I/O stay inside this package so the binary is a single static file.
package fleet

import (
	"fmt"
	"os"
	"path/filepath"
)

// Paths pins every contract file the CLI reads or writes. Locations mirror
// scripts/fleet and ci/promote.sh exactly (same file contracts).
type Paths struct {
	Root         string
	Registry     string
	Environments string
	State        string
	Gates        string
	Journal      string
	Approvals    string
	Tests        string
}

// LoadPaths resolves FLEET_ROOT (env override first, then walk-up discovery
// of ops/PROJECTS.yaml from cwd, then from the executable dir — the same
// order fleethub uses) and derives every contract path from it.
func LoadPaths() (Paths, error) {
	root, err := findRoot()
	if err != nil {
		return Paths{}, err
	}
	return Paths{
		Root:         root,
		Registry:     filepath.Join(root, "ops", "PROJECTS.yaml"),
		Environments: filepath.Join(root, "ops", "ENVIRONMENTS.yaml"),
		State:        filepath.Join(root, "ops", "state", "deployments.yaml"),
		Gates:        filepath.Join(root, "lifecycle", "gates.yaml"),
		Journal:      filepath.Join(root, "lifecycle", "journal", "events.log"),
		Approvals:    filepath.Join(root, "lifecycle", "approvals"),
		Tests:        filepath.Join(root, "tests"),
	}, nil
}

func findRoot() (string, error) {
	if r := os.Getenv("FLEET_ROOT"); r != "" && isFleetRoot(r) {
		return r, nil
	}
	if wd, err := os.Getwd(); err == nil {
		if r, ok := walkUp(wd); ok {
			return r, nil
		}
	}
	if exe, err := os.Executable(); err == nil {
		if r, ok := walkUp(filepath.Dir(exe)); ok {
			return r, nil
		}
	}
	return "", fmt.Errorf("FLEET ERROR :: no ops/PROJECTS.yaml found (set FLEET_ROOT)")
}

func walkUp(start string) (string, bool) {
	dir := start
	for {
		if isFleetRoot(dir) {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func isFleetRoot(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "ops", "PROJECTS.yaml"))
	return err == nil
}

// Run dispatches one subcommand and returns the process exit code.
// Machine contract: errors print `FLEET ERROR :: ...` on stderr, rc=1.
func Run(cmd string, args []string) int {
	switch cmd {
	case "status":
		return cmdStatus(args)
	case "doctor", "registry-check":
		return cmdDoctor(args)
	case "approve":
		return cmdApprove(args)
	case "promote":
		return cmdPromote(args)
	case "init":
		return cmdInit(args)
	case "onboard":
		return cmdOnboard(args)
	case "next":
		return cmdNext(args)
	case "wo":
		return cmdWo(args)
	case "verify":
		return cmdVerify(args)
	case "check":
		return cmdCheck(args)
	case "site":
		return cmdSite(args)
	case "infra":
		return cmdInfra(args)
	case "ops":
		return cmdOps(args)
	case "mcp":
		return cmdMcp(args)
	default:
		fmt.Fprintf(os.Stderr, "FLEET ERROR :: unknown command '%s' (see --help)\n", cmd)
		return 1
	}
}
