package fleet

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// infra deploy (WO-15): stand up the SITE INFRASTRUCTURE on a fresh
// install — docker-registry, cloudflared, gatus — from the site's
// templates, then run monitor for the dashboard/gatus config. These four
// apps are what makes a bare k3s node a fleet site; everything else
// (customer services) goes through ops register/deploy.
//
// Order matters: registry first (kaniko pushes need it), cloudflared
// second (token secret ensured from the secrets home), gatus last
// (monitor re-renders its config after it is up).

func cmdInfra(args []string) int {
	p, rc := mustPaths()
	if rc != 0 {
		return rc
	}
	sub := ""
	siteName := ""
	var tail []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--site":
			if i+1 >= len(args) {
				return failf("--site requires a value")
			}
			siteName = args[i+1]
			i++
		default:
			if strings.HasPrefix(args[i], "-") {
				return failf("unknown flag %q for infra", args[i])
			}
			if sub == "" {
				sub = args[i]
				continue
			}
			tail = append(tail, args[i])
		}
	}
	if sub != "deploy" {
		return failf("usage: fleet infra deploy [--site S]")
	}

	sites, err := LoadSites(p)
	if err != nil {
		return failf("sites registry unreadable: %v", err)
	}
	if len(sites) == 0 {
		return failf("no sites registered in ops/SITES.yaml")
	}
	var site Site
	if siteName != "" {
		var found bool
		site, found = getSite(sites, siteName)
		if !found {
			return failf("unknown site '%s'", siteName)
		}
	} else if len(sites) == 1 {
		site = sites[0]
	} else {
		return failf("multiple sites registered; pass --site")
	}
	if site.Engine != "fleet" {
		return failf("site '%s': engine '%s' — infra deploy is fleet-managed only", site.Name, site.Engine)
	}

	oc, rc := resolveOpsContext(p, site.Name)
	if rc != 0 {
		return rc
	}
	runner, cleanup, err := newKubectlRunner(site, p.Root)
	if err != nil {
		return failf("%v", err)
	}
	defer cleanup()

	// fresh-cluster guard: every site template assumes the site
	// namespace exists; on a brand-new k3s node it does not. Idempotent
	// scaffolding — get first, create only when missing.
	if out, rc := runner.Run("get", "namespace", site.Namespace); rc != 0 {
		out2, rc2 := runner.Run("create", "namespace", site.Namespace)
		if rc2 != 0 {
			return failf("ensure namespace %s: %s %s", site.Namespace, strings.TrimSpace(out), strings.TrimSpace(out2))
		}
	}

	templatesDir := filepath.Join(site.LabRootAbs(p.Root), "templates")
	order := []struct {
		file       string
		deployment string
	}{
		{"docker-registry.yaml", "docker-registry"},
		{"cloudflared.yaml", "cloudflared"},
		{"gatus.yaml", "gatus"},
	}
	applied := 0
	for _, o := range order {
		path := filepath.Join(templatesDir, o.file)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		// cloudflared needs its token secret before the deployment lands.
		// tunnel.env stores the value as TUNNEL_TOKEN= (site tunnel create)
		// while the template's secretKeyRef reads key "token" — stage a
		// mapped env file so the secret carries the key the pod wants.
		if o.deployment == "cloudflared" {
			tunnelEnv := filepath.Join(site.secretsDir(p.Root), "tunnel.env")
			if _, err := os.Stat(tunnelEnv); err == nil {
				raw, err := os.ReadFile(tunnelEnv)
				if err != nil {
					return failf("%v", err)
				}
				tok := ""
				for _, line := range strings.Split(string(raw), "\n") {
					if v, ok := strings.CutPrefix(line, "TUNNEL_TOKEN="); ok {
						tok = strings.TrimSpace(v)
					}
				}
				if tok == "" {
					return failf("tunnel.env has no TUNNEL_TOKEN= line")
				}
				mapped, err := os.CreateTemp("", "fleet-tunnel-token-*.env")
				if err != nil {
					return failf("%v", err)
				}
				defer os.Remove(mapped.Name())
				if err := os.WriteFile(mapped.Name(), []byte("token="+tok+"\n"), 0o600); err != nil {
					return failf("%v", err)
				}
				if err := labEnsureSecret(runner, site.Namespace, "cloudflared-token", mapped.Name()); err != nil {
					return failf("%v", err)
				}
			} else {
				fmt.Printf("INFRA WARN site=%s no tunnel.env in the secrets home — cloudflared will CrashLoop until `site tunnel create` stores it\n", site.Name)
			}
		}
		if out, rc2 := runner.Run("apply", "-f", path); rc2 != 0 {
			return failf("apply %s: %s", o.file, strings.TrimSpace(out))
		}
		applied++
		if out, rc2 := runner.Run("rollout", "status", "-n", site.Namespace,
			"--timeout=180s", "deployment/"+o.deployment); rc2 != 0 {
			return failf("rollout %s: %s", o.deployment, strings.TrimSpace(out))
		}
	}
	if applied == 0 {
		return failf("no infra templates found under %s (site new scaffolds them)", templatesDir)
	}

	// monitor syncs the gatus + dashboard config for everything registered
	if rc := opsMonitor(oc, tail); rc != 0 {
		return rc
	}

	AppendJournal(p.Journal, fmt.Sprintf(
		"# infra-deploy wo=%s site=%s applied=%d",
		os.Getenv("FLEET_WO"), site.Name, applied))

	fmt.Printf("INFRA OK site=%s applied=%d\n", site.Name, applied)
	fmt.Printf("next: ./scripts/fleet site canary --site %s  (proves build→deploy→verify→remove end to end)\n", site.Name)
	return 0
}
