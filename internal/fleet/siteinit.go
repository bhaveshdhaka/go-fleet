package fleet

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Site migration (WO-9): `fleet site init <name> --from <lab_root>`
// imports the predecessor engine's site data (registry, state,
// templates) into fleet's git at ops/sites/<name>, archives the as-
// migrated operational history, and flips the SITES.yaml entry to
// engine: fleet. Secret FILES are never copied or moved — the migrated
// site references them via secrets_dir (values stay gitignored in the
// predecessor tree).

func validSiteEngine(e string) bool {
	return e == "sos-lab" || e == "fleet"
}

var siteNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

func cmdSiteInit(args []string) int {
	p, rc := mustPaths()
	if rc != 0 {
		return rc
	}
	name := ""
	var from string
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--from":
			if i+1 >= len(args) {
				return failf("--from requires a value")
			}
			from = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--from="):
			from = strings.TrimPrefix(args[i], "--from=")
		default:
			if strings.HasPrefix(args[i], "-") {
				return failf("unknown flag %q for site init", args[i])
			}
			if name != "" {
				return failf("site init takes exactly one name")
			}
			name = args[i]
		}
	}
	if name == "" || from == "" {
		return failf("usage: fleet site init <name> --from <lab_root>")
	}
	if !siteNameRe.MatchString(name) {
		return failf("site name must match %s", siteNameRe.String())
	}
	srcRoot := from
	if !filepath.IsAbs(srcRoot) {
		srcRoot = filepath.Join(p.Root, srcRoot)
	}
	srcRoot = filepath.Clean(srcRoot)
	for _, req := range []string{"config/registry.yaml", "templates"} {
		if _, err := os.Stat(filepath.Join(srcRoot, req)); err != nil {
			return failf("source lab root %s is missing %s", srcRoot, req)
		}
	}
	dst := filepath.Join(p.Root, "ops", "sites", name)
	if _, err := os.Stat(dst); err == nil {
		return failf("site data dir already exists: %s", dst)
	}
	sites, err := LoadSites(p)
	if err != nil {
		return failf("sites registry unreadable: %v", err)
	}
	existing, found := getSite(sites, name)
	if found {
		if !validSiteEngine(existing.Engine) {
			return failf("site '%s': engine '%s' not supported yet", name, existing.Engine)
		}
		if existing.Engine == "fleet" {
			return failf("site '%s' is already fleet-managed", name)
		}
	}

	// import
	if err := os.MkdirAll(filepath.Join(dst, "config"), 0o755); err != nil {
		return failf("%v", err)
	}
	if err := copyFile(filepath.Join(srcRoot, "config", "registry.yaml"),
		filepath.Join(dst, "config", "registry.yaml")); err != nil {
		return failf("%v", err)
	}
	if err := copyDirContents(filepath.Join(srcRoot, "state"), filepath.Join(dst, "state"), ".json"); err != nil {
		return failf("%v", err)
	}
	if err := copyDirContents(filepath.Join(srcRoot, "templates"), filepath.Join(dst, "templates"), ""); err != nil {
		return failf("%v", err)
	}
	// imported registry must validate under the same contract
	site := Site{Name: name, Engine: "fleet", LabRoot: filepath.Join("ops", "sites", name),
		Namespace: existing.Namespace, Access: existing.Access, SecretsDir: existing.SecretsDir}
	if site.Namespace == "" {
		site.Namespace = "sos-lab"
	}
	if site.Access == "" {
		site.Access = "in-cluster"
	}
	if _, err := LoadLabView(site, p.Root); err != nil {
		os.RemoveAll(dst)
		return failf("imported registry failed validation: %v", err)
	}

	// archive the as-migrated operational history
	arch := filepath.Join(dst, "archive")
	if err := os.MkdirAll(arch, 0o755); err != nil {
		return failf("%v", err)
	}
	depEntries, bldEntries := 0, 0
	var depBytes, bldBytes []byte
	if raw, err := os.ReadFile(filepath.Join(dst, "state", "deployed.json")); err == nil {
		depBytes = raw
		if m, err := loadJSONFile(filepath.Join(dst, "state", "deployed.json")); err == nil {
			depEntries = len(m)
		}
	}
	if raw, err := os.ReadFile(filepath.Join(dst, "state", "builds.json")); err == nil {
		bldBytes = raw
		if m, err := loadJSONFile(filepath.Join(dst, "state", "builds.json")); err == nil {
			bldEntries = len(m)
		}
	}
	if len(depBytes) > 0 {
		if err := os.WriteFile(filepath.Join(arch, "deployed.json"), depBytes, 0o644); err != nil {
			return failf("%v", err)
		}
	}
	if len(bldBytes) > 0 {
		if err := os.WriteFile(filepath.Join(arch, "builds.json"), bldBytes, 0o644); err != nil {
			return failf("%v", err)
		}
	}
	manifest := fmt.Sprintf(`# MIGRATION MANIFEST — site %s
migrated_at: %s
source: %s
source_engine: %s
source_git_sha: %s
imported:
  config/registry.yaml: byte-preserved
  state/deployed.json: %d entries
  state/builds.json: %d entries
  templates/: file-for-file
archive:
  deployed.json: state snapshot as of migration
  builds.json: build history as of migration
secrets: NOT copied — referenced via secrets_dir (gitignored at source)
authoritative_engine: fleet (cutover at migration)
`, name, FleetTS(time.Now()), srcRoot, existing.Engine, labGitSha(srcRoot), depEntries, bldEntries)
	if err := os.WriteFile(filepath.Join(arch, "MIGRATION.md"), []byte(manifest), 0o644); err != nil {
		return failf("%v", err)
	}

	// SITES.yaml cutover (anchored edit of the site entry, validated by
	// re-parse)
	secretsDirRel := pathRelToRoot(p.Root, filepath.Join(srcRoot, "secrets"))
	if err := sitesYamlCutover(p, name, existing, found, secretsDirRel); err != nil {
		return failf("%v", err)
	}

	AppendJournal(p.Journal, fmt.Sprintf(
		"# site-init wo=%s site=%s from=%s engine=fleet lab_root=ops/sites/%s secrets_dir=%s services_imported=1 state_entries=%d+%d archive=written secrets_not_copied=true",
		os.Getenv("FLEET_WO"), name, srcRoot, name, secretsDirRel, depEntries, bldEntries))

	fmt.Printf("SITE INIT site=%s engine=fleet lab_root=ops/sites/%s secrets_dir=%s archive=%s\n",
		name, name, secretsDirRel, filepath.Join("ops", "sites", name, "archive"))
	return 0
}

// sitesYamlCutover flips (or appends) the site entry in ops/SITES.yaml.
func sitesYamlCutover(p Paths, name string, existing Site, found bool, secretsDirRel string) error {
	path := filepath.Join(p.Root, "ops", "SITES.yaml")
	lines, err := readLines(path)
	if err != nil {
		return err
	}
	labRootRel := "ops/sites/" + name
	var out []string
	if found {
		start, end, ok := siteEntryBlock(lines, name)
		if !ok {
			return fmt.Errorf("site '%s' entry not found in SITES.yaml", name)
		}
		out = append(out, lines[:start]...)
		entry := []string{
			"  - name: " + name,
			"    engine: fleet",
			"    lab_root: " + labRootRel,
		}
		ns := existing.Namespace
		if ns == "" {
			ns = "sos-lab"
		}
		entry = append(entry, "    namespace: "+ns)
		acc := existing.Access
		if acc == "" {
			acc = "in-cluster"
		}
		entry = append(entry, "    access: "+acc)
		entry = append(entry, "    secrets_dir: "+secretsDirRel)
		entry = append(entry, `    description: fleet-managed site (migrated `+FleetTS(time.Now())+`)`)
		out = append(out, entry...)
		out = append(out, lines[end:]...)
	} else {
		out = append(out, lines...)
		out = append(out,
			"",
			"  - name: "+name,
			"    engine: fleet",
			"    lab_root: "+labRootRel,
			"    namespace: sos-lab",
			"    access: in-cluster",
			"    secrets_dir: "+secretsDirRel,
			"    description: fleet-managed site (migrated "+FleetTS(time.Now())+")")
	}
	text := strings.Join(out, "\n")
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	// write atomically, then validate by re-parse from disk
	tmp := path + ".fleet.tmp"
	if err := os.WriteFile(tmp, []byte(text), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	sites, err := LoadSites(p)
	if err != nil {
		return fmt.Errorf("SITES.yaml edit failed validation: %v", err)
	}
	s, ok := getSite(sites, name)
	if !ok || s.Engine != "fleet" || s.LabRoot != labRootRel || s.SecretsDir != secretsDirRel {
		return fmt.Errorf("SITES.yaml cutover did not take effect for '%s'", name)
	}
	return nil
}

// siteEntryBlock locates one site entry ("  - name: <n>" until the next
// "  - name:" or EOF), including blank lines before the next entry.
func siteEntryBlock(lines []string, name string) (start, end int, ok bool) {
	found := false
	for i, ln := range lines {
		if strings.TrimRight(ln, " ") == "  - name: "+name {
			start, found = i, true
			break
		}
	}
	if !found {
		return 0, 0, false
	}
	end = len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "  - name: ") {
			end = i
			break
		}
	}
	return start, end, true
}

// pathRelToRoot renders a path relative to the fleet root when possible
// (keeps SITES.yaml portable — including "../sos-lab/secrets" style
// references to the predecessor tree), else absolute.
func pathRelToRoot(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil {
		return rel
	}
	return path
}

func copyFile(src, dst string) error {
	raw, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, raw, 0o644)
}

// copyDirContents copies regular files (optionally filtered by suffix)
// from src to dst, creating dst.
func copyDirContents(src, dst, suffix string) error {
	ents, err := os.ReadDir(src)
	if err != nil {
		if os.IsNotExist(err) && suffix == ".json" {
			return os.MkdirAll(dst, 0o755)
		}
		return err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	for _, e := range ents {
		if e.IsDir() || (suffix != "" && !strings.HasSuffix(e.Name(), suffix)) {
			continue
		}
		if err := copyFile(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return err
		}
	}
	return nil
}
