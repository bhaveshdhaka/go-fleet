package fleet

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// sos-lab contract readers (WO-7): registry.yaml (schema v2, validated the
// same way labctl/registry.py validates), state/deployed.json,
// state/builds.json, and secret-file KEY-NAME checks. Secret VALUES are
// never read into memory beyond the line being inspected for the key name.

const (
	LabRegistryHost = "docker-registry.sos-lab.svc.cluster.local:5000"
	LabKanikoImage  = "gcr.io/kaniko-project/executor:v1.23.2"
	LabKubeVersion  = "v1.36.3" // labctl KUBE_VERSION (overlay build-arg)
)

type LabView struct {
	Site     Site
	Registry map[string]any
	Deployed map[string]any
	Builds   map[string]any
}

func labPath(s Site, root, rel string) string {
	return filepath.Join(s.LabRootAbs(root), rel)
}

// LoadLabView parses and validates the sos-lab contract files for a site.
func LoadLabView(s Site, root string) (*LabView, error) {
	raw, err := os.ReadFile(labPath(s, root, "config/registry.yaml"))
	if err != nil {
		return nil, fmt.Errorf("registry unreadable: %v", err)
	}
	top, err := parseMiniYAML(string(raw))
	if err != nil {
		return nil, fmt.Errorf("registry.yaml: %v", err)
	}
	reg := asMap(top)
	if reg == nil {
		return nil, fmt.Errorf("registry.yaml: top level must be a mapping")
	}
	if err := validateLabRegistry(reg); err != nil {
		return nil, err
	}
	lv := &LabView{Site: s, Registry: reg}
	if lv.Deployed, err = loadJSONFile(labPath(s, root, "state/deployed.json")); err != nil {
		return nil, err
	}
	if lv.Builds, err = loadJSONFile(labPath(s, root, "state/builds.json")); err != nil {
		return nil, err
	}
	return lv, nil
}

func loadJSONFile(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("%s unreadable: %v", path, err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("%s is corrupt: %v", path, err)
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

// validateLabRegistry mirrors labctl/registry.py validate()/validate_service().
func validateLabRegistry(reg map[string]any) error {
	for _, section := range []string{"cloudflare", "domains", "services"} {
		if asMap(reg[section]) == nil {
			return fmt.Errorf("registry.yaml: missing/invalid section '%s'", section)
		}
	}
	cf := asMap(reg["cloudflare"])
	for _, k := range []string{"account_id", "tunnel_id", "tunnel_name"} {
		if asString(cf[k]) == "" {
			return fmt.Errorf("registry.yaml: cloudflare.%s required", k)
		}
	}
	for name, svcAny := range asMap(reg["services"]) {
		svc := asMap(svcAny)
		if svc == nil {
			return fmt.Errorf("service '%s': must be a mapping", name)
		}
		port, ok := svc["port"].(int)
		if !ok || port <= 0 || port > 65535 {
			return fmt.Errorf("service '%s': port must be an integer 1-65535", name)
		}
		if asString(svc["namespace"]) == "" {
			return fmt.Errorf("service '%s': missing field 'namespace'", name)
		}
		if asString(svc["image"]) == "" && asString(svc["repo"]) == "" {
			return fmt.Errorf("service '%s': needs 'image' or 'repo'", name)
		}
		if svc["enabled"] != nil {
			if _, isBool := svc["enabled"].(bool); !isBool {
				return fmt.Errorf("service '%s': enabled must be a boolean", name)
			}
		}
	}
	for dom, cfgAny := range asMap(reg["domains"]) {
		if asString(asMap(cfgAny)["zone_id"]) == "" {
			return fmt.Errorf("registry.yaml: domains.%s.zone_id required", dom)
		}
	}
	return nil
}

// LabServices returns the services map.
func (lv *LabView) LabServices() map[string]map[string]any {
	out := map[string]map[string]any{}
	for name, svcAny := range asMap(lv.Registry["services"]) {
		if m := asMap(svcAny); m != nil {
			out[name] = m
		}
	}
	return out
}

// LabServiceNames returns service names sorted.
func (lv *LabView) LabServiceNames() []string {
	var out []string
	for name := range lv.LabServices() {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// RoutedServices mirrors registry.routed_services: enabled + host, sorted.
func (lv *LabView) RoutedServices() []struct {
	Name string
	Svc  map[string]any
} {
	var out []struct {
		Name string
		Svc  map[string]any
	}
	for _, name := range lv.LabServiceNames() {
		svc := lv.LabServices()[name]
		if asBool(svc["enabled"]) && asString(svc["host"]) != "" {
			out = append(out, struct {
				Name string
				Svc  map[string]any
			}{name, svc})
		}
	}
	return out
}

// ZoneOfHost mirrors registry.zone_of_host: longest-suffix domain match.
func (lv *LabView) ZoneOfHost(host string) string {
	best := ""
	zid := ""
	for dom, cfgAny := range asMap(lv.Registry["domains"]) {
		if host == dom || strings.HasSuffix(host, "."+dom) {
			if len(dom) > len(best) {
				best = dom
				zid = asString(asMap(cfgAny)["zone_id"])
			}
		}
	}
	return zid
}

// TunnelTarget mirrors registry.tunnel_target.
func (lv *LabView) TunnelTarget() string {
	return asString(asMap(lv.Registry["cloudflare"])["tunnel_id"]) + ".cfargotunnel.com"
}

// StateEntryString pulls a top-level string field from a state entry.
func stateEntry(m map[string]any, svc, field string) string {
	return asString(asMap(m[svc])[field])
}

// DeployedTag mirrors status column: deployed tag or "-".
func (lv *LabView) DeployedTag(svc string) string {
	if t := stateEntry(lv.Deployed, svc, "tag"); t != "" {
		return t
	}
	return "-"
}

// StatusSHA mirrors status column: deployed.git_sha or builds.git_sha[:8] or "-".
func (lv *LabView) StatusSHA(svc string) string {
	sha := stateEntry(lv.Deployed, svc, "git_sha")
	if sha == "" {
		sha = stateEntry(lv.Builds, svc, "git_sha")
	}
	if sha == "" {
		return "-"
	}
	if len(sha) > 8 {
		sha = sha[:8]
	}
	return sha
}

// CheckSecrets mirrors registry.check_secrets: verifies KEY NAMES exist in
// secrets/<svc>.env; the file may hold the values — this code only looks
// at the left-hand side of '=' and never stores or prints values.
func (lv *LabView) CheckSecrets(name string, svc map[string]any, root string) string {
	want := asList(svc["secrets"])
	if len(want) == 0 {
		return ""
	}
	path := filepath.Join(lv.Site.secretsDir(root), name+".env")
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("missing file %s (needs keys: %s)", path, joinNames(want))
	}
	have := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		if i := strings.Index(line, "="); i > 0 {
			have[strings.TrimSpace(line[:i])] = true
		}
	}
	var missing []string
	for _, k := range want {
		if !have[asString(k)] {
			missing = append(missing, asString(k))
		}
	}
	if len(missing) > 0 {
		return fmt.Sprintf("%s missing keys: %s", path, strings.Join(missing, ", "))
	}
	return ""
}

func joinNames(items []any) string {
	var out []string
	for _, i := range items {
		out = append(out, asString(i))
	}
	return strings.Join(out, ", ")
}

// LoadCloudflareToken mirrors cloudflare.load_token: CF_API_TOKEN from
// the site's secrets dir (honoring the secrets_dir override). The token
// never leaves this function except to the HTTP client.
func LoadCloudflareToken(secretsDir string) (string, error) {
	path := filepath.Join(secretsDir, "cloudflare.env")
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("missing %s", path)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "CF_API_TOKEN=") {
			tok := strings.Trim(strings.TrimPrefix(line, "CF_API_TOKEN="), "\"'")
			if tok != "" {
				return tok, nil
			}
		}
	}
	return "", fmt.Errorf("CF_API_TOKEN not set in %s", path)
}
