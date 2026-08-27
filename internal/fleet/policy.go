package fleet

import (
	"os"
	"path/filepath"
	"strings"
)

// .fleet.yaml — fleet policy (WO-5). Only the approvals block is parsed:
//   approvals:
//     allowed_actors: [owner, owner-via-agent]
//     require_human_stages: [prod]
// Missing file = unrestricted (backward compatible). Actors outside
// allowed_actors are refused on require_human_stages with the exact fix.

type Policy struct {
	Present            bool
	AllowedActors      []string
	RequireHumanStages []string
}

func loadPolicy(root string) Policy {
	b, err := os.ReadFile(filepath.Join(root, ".fleet.yaml"))
	if err != nil {
		return Policy{}
	}
	var pol Policy
	inApprovals := false
	curList := ""
	for _, raw := range strings.Split(string(b), "\n") {
		ln := strings.TrimRight(raw, "\r")
		switch {
		case strings.HasPrefix(ln, "approvals:"):
			pol.Present = true
			inApprovals = true
			curList = ""
		case inApprovals && strings.HasPrefix(ln, "  allowed_actors:"):
			pol.AllowedActors = parseListValue(ln, "allowed_actors:")
			curList = "actors"
		case inApprovals && strings.HasPrefix(ln, "  require_human_stages:"):
			pol.RequireHumanStages = parseListValue(ln, "require_human_stages:")
			curList = "stages"
		case inApprovals && strings.HasPrefix(ln, "    - "):
			item := strings.TrimSpace(strings.TrimPrefix(ln, "    - "))
			switch curList {
			case "actors":
				pol.AllowedActors = append(pol.AllowedActors, item)
			case "stages":
				pol.RequireHumanStages = append(pol.RequireHumanStages, item)
			}
		}
	}
	return pol
}

// parseListValue reads an inline `[a, b]` value (empty [] allowed).
func parseListValue(ln, key string) []string {
	v := strings.TrimSpace(strings.TrimPrefix(ln, "  "+key))
	v = strings.TrimPrefix(v, "[")
	v = strings.TrimSuffix(v, "]")
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(v, ",") {
		s := strings.Trim(strings.TrimSpace(part), "\"'")
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func actorAllowed(pol Policy, actor, stage string) bool {
	if !pol.Present {
		return true
	}
	if len(pol.RequireHumanStages) == 0 {
		return true
	}
	human := false
	for _, s := range pol.RequireHumanStages {
		if s == stage {
			human = true
			break
		}
	}
	if !human {
		return true
	}
	for _, a := range pol.AllowedActors {
		if a == actor {
			return true
		}
	}
	return false
}
