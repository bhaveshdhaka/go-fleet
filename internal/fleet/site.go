package fleet

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Site model (WO-7): a site is an externally managed deployment target.
// The registry file is ops/SITES.yaml; access is always EXPLICIT — the ops
// runner refuses to touch a cluster based on ambient environment state.

type Site struct {
	Name        string
	Engine      string
	LabRoot     string
	Namespace   string
	Access      string
	Description string
	// SecretsDir optionally overrides where the site engine keeps its
	// gitignored secret env files. Set by site init so a migrated site can
	// reference the predecessor's secrets WITHOUT copying them (WO-9:
	// "secrets untouched").
	SecretsDir string
}

func validSiteAccess(a string) bool {
	return a == "in-cluster" || strings.HasPrefix(a, "kubeconfig:")
}

// LoadSites parses ops/SITES.yaml (same block grammar as the registry).
func LoadSites(p Paths) ([]Site, error) {
	lines, err := readLines(filepath.Join(p.Root, "ops", "SITES.yaml"))
	if err != nil {
		return nil, err
	}
	var out []Site
	var cur *Site
	for _, ln := range lines {
		if strings.HasPrefix(ln, "  - name: ") {
			if cur != nil {
				out = append(out, *cur)
			}
			cur = &Site{Name: strings.TrimPrefix(ln, "  - name: ")}
			continue
		}
		if cur == nil || !strings.HasPrefix(ln, "    ") {
			continue
		}
		kv := strings.SplitN(strings.TrimSpace(ln), ":", 2)
		if len(kv) != 2 {
			continue
		}
		val := strings.TrimSpace(kv[1])
		switch kv[0] {
		case "engine":
			cur.Engine = val
		case "lab_root":
			cur.LabRoot = val
		case "namespace":
			cur.Namespace = val
		case "access":
			cur.Access = val
		case "description":
			cur.Description = strings.Trim(val, `"`)
		case "secrets_dir":
			cur.SecretsDir = val
		}
	}
	if cur != nil {
		out = append(out, *cur)
	}
	return out, nil
}

// SiteLabRoot is the absolute path of a site's engine repo.
func (s Site) LabRootAbs(root string) string {
	if filepath.IsAbs(s.LabRoot) {
		return s.LabRoot
	}
	return filepath.Join(root, s.LabRoot)
}

// SiteKubeconfig resolves the explicit kubeconfig path for kubeconfig-style
// access; "" for in-cluster (the runner materializes one from the site
// declaration, never from the ambient environment).
func (s Site) SiteKubeconfig(root string) string {
	if strings.HasPrefix(s.Access, "kubeconfig:") {
		p := strings.TrimPrefix(s.Access, "kubeconfig:")
		if filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(s.LabRootAbs(root), p)
	}
	return ""
}

func getSite(sites []Site, name string) (Site, bool) {
	for _, s := range sites {
		if s.Name == name {
			return s, true
		}
	}
	return Site{}, false
}

func cmdSite(args []string) int {
	p, rc := mustPaths()
	if rc != 0 {
		return rc
	}
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	if sub == "init" {
		return cmdSiteInit(args[1:])
	}
	if sub != "list" && sub != "" {
		return failf("unknown site subcommand '%s' (list|init)", sub)
	}
	sites, err := LoadSites(p)
	if err != nil {
		return failf("sites registry unreadable: %v", err)
	}
	fmt.Printf("SITE LIST count=%d\n", len(sites))
	for _, s := range sites {
		acc := s.Access
		if !validSiteAccess(acc) {
			acc = "invalid:" + acc
		}
		fmt.Printf("SITE name=%s engine=%s access=%s lab_root=%s\n",
			s.Name, s.Engine, acc, s.LabRoot)
	}
	return 0
}

// siteSecretsDir is where the site engine keeps its gitignored secret env
// files: the secrets_dir override when declared (migrated sites reference
// the predecessor's gitignored dir — values are never copied), otherwise
// <lab_root>/secrets.
func (s Site) secretsDir(root string) string {
	if s.SecretsDir != "" {
		if filepath.IsAbs(s.SecretsDir) {
			return s.SecretsDir
		}
		return filepath.Join(root, s.SecretsDir)
	}
	return filepath.Join(s.LabRootAbs(root), "secrets")
}
