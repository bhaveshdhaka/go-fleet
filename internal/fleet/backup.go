package fleet

import (
	"encoding/json"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Backup & restore (WO-20): the fleet-way insurance policy.
//
// Tool: restic (Go, S3-compatible → Cloudflare R2, deduplicating,
// encrypted with ONE password the owner keeps in a password manager).
//
// Split of labor (deliberate):
//   - in-cluster Jobs move DATA: official restic/restic image, the
//     service's PVC mounted at /srv/backup, command = plain restic args
//     (no shell in the image, no custom image to build)
//   - the HOST orchestrates: forget/prune (retention), snapshot listing,
//     and the secrets-home backup (host has restic from .toolchain)
//
// Site registry gains a top-level section:
//
//	backup:
//	  bucket: fleet-backups
//	  endpoint: https://<account>.r2.cloudflarestorage.com
//	  repo: <site-name>
//	  schedule: nightly|weekly|none   (optional, default none)
//	  retention: --keep-daily 7 --keep-weekly 4   (optional)
//
// Secrets live in <secrets home>/r2.env: R2_ACCESS_KEY_ID,
// R2_SECRET_ACCESS_KEY (R2 → Manage API Tokens → Object Read & Write)
// and RESTIC_PASSWORD (fleet-generated at init, shown once).

const resticImage = "restic/restic:0.17.3"

type backupConfig struct {
	Bucket    string
	Endpoint  string
	Repo      string
	Schedule  string
	Retention string
}

// loadBackupConfig parses the registry's backup: section (absent → nil).
func loadBackupConfig(lv *LabView) *backupConfig {
	m := asMap(lv.Registry["backup"])
	if m == nil {
		return nil
	}
	cfg := &backupConfig{
		Bucket:    asString(m["bucket"]),
		Endpoint:  asString(m["endpoint"]),
		Repo:      asString(m["repo"]),
		Schedule:  asString(m["schedule"]),
		Retention: asString(m["retention"]),
	}
	if cfg.Retention == "" {
		cfg.Retention = "--keep-daily 7 --keep-weekly 4"
	}
	if cfg.Bucket == "" || cfg.Endpoint == "" || cfg.Repo == "" {
		return nil
	}
	return cfg
}

type r2Creds struct {
	AccessKey string
	SecretKey string
	Password  string
}

// loadR2Creds reads the secrets home's r2.env (key names only ever
// surface in errors; values never printed).
func loadR2Creds(secretsDir string) (*r2Creds, error) {
	raw, err := os.ReadFile(filepath.Join(secretsDir, "r2.env"))
	if err != nil {
		return nil, fmt.Errorf("missing %s — run: ./scripts/fleet ops backup init", filepath.Join(secretsDir, "r2.env"))
	}
	c := &r2Creds{}
	for _, ln := range strings.Split(string(raw), "\n") {
		ln = strings.TrimSpace(ln)
		if i := strings.Index(ln, "="); i > 0 {
			k, v := ln[:i], strings.Trim(strings.TrimPrefix(ln[i+1:], " "), "\"'")
			switch k {
			case "R2_ACCESS_KEY_ID":
				c.AccessKey = v
			case "R2_SECRET_ACCESS_KEY":
				c.SecretKey = v
			case "RESTIC_PASSWORD":
				c.Password = v
			}
		}
	}
	if c.AccessKey == "" || c.SecretKey == "" || c.Password == "" {
		return nil, fmt.Errorf("r2.env incomplete (need R2_ACCESS_KEY_ID, R2_SECRET_ACCESS_KEY, RESTIC_PASSWORD)")
	}
	return c, nil
}

// repoString renders the restic repository location: s3 endpoint for
// production; a bare local path when Endpoint is empty (hermetic
// round-trip tests drive real restic against a local dir).
func repoString(cfg *backupConfig) string {
	if cfg.Endpoint == "" {
		return cfg.Repo
	}
	return fmt.Sprintf("s3:%s/%s/%s", cfg.Endpoint, cfg.Bucket, cfg.Repo)
}

func resticEnv(c *r2Creds, cfg *backupConfig) []string {
	return []string{
		"RESTIC_REPOSITORY=" + repoString(cfg),
		"RESTIC_PASSWORD=" + c.Password,
		"AWS_ACCESS_KEY_ID=" + c.AccessKey,
		"AWS_SECRET_ACCESS_KEY=" + c.SecretKey,
	}
}

// hostRestic runs restic on the host (secrets-home backup, forget/prune,
// snapshot listing). Resolution: FLEET_RESTIC_BIN (hermetic test seam) →
// toolchain-pinned binary → PATH.
func hostRestic(c *r2Creds, cfg *backupConfig, args ...string) (string, error) {
	bin := os.Getenv("FLEET_RESTIC_BIN")
	if bin == "" {
		bin = "/home/openchamber/workspaces/.toolchain/bin/restic"
		if _, err := os.Stat(bin); err != nil {
			if alt, e := exec.LookPath("restic"); e == nil {
				bin = alt
			} else {
				return "", fmt.Errorf("restic not found (toolchain .toolchain/bin/restic missing)")
			}
		}
	}
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ()[:0:0], resticEnv(c, cfg)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("restic %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// --- ops backup init -------------------------------------------------------

func opsBackupInit(oc *opsContext, args []string) int {
	bucket := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--bucket":
			if i+1 >= len(args) {
				return opError(fmt.Errorf("--bucket requires a value"))
			}
			bucket = args[i+1]
			i++
		default:
			return opError(fmt.Errorf("unknown argument %q for ops backup init", args[i]))
		}
	}
	if bucket == "" {
		return opError(fmt.Errorf("usage: fleet ops backup init --site S --bucket <name>"))
	}
	// 1. account id comes from the registry (cloudflare.account_id — the
	// same id as the R2 S3 endpoint). No /accounts listing needed; tokens
	// are only used for the bucket-create call (R2 Edit).
	accountID := asString(asMap(oc.lv.Registry["cloudflare"])["account_id"])
	if accountID == "" {
		return opError(fmt.Errorf("registry cloudflare.account_id missing"))
	}
	// 2. create the bucket via the CF API (idempotent: exists → ok);
	// writes use the R2-scoped token when present
	if _, err := cloudflareAPIBody(cloudflareTokenFor(oc, accountID), "POST",
		"/accounts/"+accountID+"/r2/buckets",
		map[string]any{"name": bucket}); err != nil {
		if !strings.Contains(err.Error(), "already exists") && !strings.Contains(err.Error(), "10004") {
			// non-fatal: the bucket may already exist (dashboard-created);
			// the first restic backup fails loudly if it truly doesn't
			fmt.Fprintf(oc.stdout, "BACKUP INIT WARN bucket create not confirmed via API (%v) — assuming it exists\n", err)
		}
	}
	// 3. registry backup section (anchored append + revalidate)
	endpoint := fmt.Sprintf("https://%s.r2.cloudflarestorage.com", accountID)
	if err := labBackupRecord(oc, bucket, endpoint); err != nil {
		return opError(err)
	}
	// 4. r2.env skeleton (RESTIC_PASSWORD generated; keys filled by owner)
	r2Path := filepath.Join(oc.site.secretsDir(oc.p.Root), "r2.env")
	if _, err := os.Stat(r2Path); err != nil {
		buf := make([]byte, 24)
		if _, err := rand.Read(buf); err != nil {
			return opError(err)
		}
		pw := base64.RawStdEncoding.EncodeToString(buf)
		body := fmt.Sprintf("R2_ACCESS_KEY_ID=\nR2_SECRET_ACCESS_KEY=\nRESTIC_PASSWORD=%s\nR2_ENDPOINT=%s\nRESTIC_BUCKET=%s\n", pw, endpoint, bucket)
		if err := os.WriteFile(r2Path, []byte(body), 0o600); err != nil {
			return opError(err)
		}
		fmt.Fprintf(oc.stdout, "BACKUP INIT wrote %s\n", r2Path)
		fmt.Fprintf(oc.stdout, "ACTION REQUIRED (once): 1) create R2 API-token keys (dashboard → R2 → Manage API tokens → Object Read & Write) and fill R2_ACCESS_KEY_ID/R2_SECRET_ACCESS_KEY in that file; 2) store RESTIC_PASSWORD + the R2 keys in your password manager — the bucket is encrypted with it\n")
	}
	AppendJournal(oc.p.Journal, fmt.Sprintf(
		"# backup-init wo=%s site=%s bucket=%s endpoint=%s", os.Getenv("FLEET_WO"), oc.site.Name, bucket, endpoint))
	fmt.Fprintf(oc.stdout, "BACKUP INIT site=%s bucket=%s repo=%s\n", oc.site.Name, bucket, oc.site.Name)
	return 0
}

// cloudflareTokenFor returns the token that could see the account (the
// R2-scoped one wins for writes; the DNS token is the fallback).
func cloudflareTokenFor(oc *opsContext, accountID string) string {
	if raw, err := os.ReadFile(filepath.Join(oc.site.secretsDir(oc.p.Root), "r2.env")); err == nil {
		for _, ln := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(strings.TrimSpace(ln), "R2_CF_TOKEN=") {
				return strings.Trim(strings.TrimPrefix(strings.TrimSpace(ln), "R2_CF_TOKEN="), "\"'")
			}
		}
	}
	t, _ := LoadCloudflareToken(oc.site.secretsDir(oc.p.Root))
	return t
}

// labBackupRecord appends/updates the registry's backup: section.
func labBackupRecord(oc *opsContext, bucket, endpoint string) error {
	path := filepath.Join(oc.labRoot, "config", "registry.yaml")
	lines, err := readLines(path)
	if err != nil {
		return err
	}
	for _, ln := range lines {
		if strings.HasPrefix(ln, "backup:") {
			return fmt.Errorf("registry already has a backup: section")
		}
	}
	section := []string{
		"backup:",
		"  bucket: " + bucket,
		"  endpoint: " + endpoint,
		"  repo: " + oc.site.Name,
		"  schedule: none",
	}
	out := append([]string{}, lines...)
	if len(out) > 0 && out[len(out)-1] != "" {
		out = append(out, "")
	}
	out = append(out, section...)
	text := strings.Join(out, "\n")
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	tmp := path + ".fleet.tmp"
	if err := os.WriteFile(tmp, []byte(text), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	if _, err := LoadLabView(oc.site, oc.p.Root); err != nil {
		return fmt.Errorf("registry edit failed validation: %v", err)
	}
	return nil
}

// --- ops backup ------------------------------------------------------------

func opsBackup(oc *opsContext, args []string) int {
	live := false
	schedule := ""
	var services []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--live":
			live = true
		case "--schedule":
			if i+1 >= len(args) {
				return opError(fmt.Errorf("--schedule requires nightly|weekly|none"))
			}
			schedule = args[i+1]
			i++
		default:
			if strings.HasPrefix(args[i], "-") {
				return opError(fmt.Errorf("unknown flag %q for ops backup", args[i]))
			}
			services = append(services, args[i])
		}
	}
	cfg := loadBackupConfig(oc.lv)
	if cfg == nil {
		return opError(fmt.Errorf("site '%s' has no backup: section — run ops backup init", oc.site.Name))
	}
	creds, err := loadR2Creds(oc.site.secretsDir(oc.p.Root))
	if err != nil {
		return opError(err)
	}
	// first run: initialize the restic repo if absent (idempotent)
	if _, err := hostRestic(creds, cfg, "cat", "config"); err != nil {
		if _, err := hostRestic(creds, cfg, "init"); err != nil {
			if !strings.Contains(err.Error(), "already exists") &&
				!strings.Contains(err.Error(), "already initialized") {
				return opError(fmt.Errorf("repo init: %v", err))
			}
		}
	}
	if schedule != "" {
		return opsBackupSchedule(oc, cfg, schedule)
	}

	// which services: EXPLICIT names or --all. A blanket sweep of a live
	// site is never implicit — WO-20 incident lesson (openchamber 502).
	all := false
	if len(services) == 0 {
		for _, a := range args {
			if a == "--all" {
				all = true
			}
		}
		if !all {
			return opError(fmt.Errorf(
				"name the services to back up (e.g. ops backup nzbdav aiostreams) or pass --all — a live site is never swept implicitly"))
		}
		for _, name := range oc.lv.LabServiceNames() {
			svc := oc.lv.LabServices()[name]
			if asMap(svc["storage"]) == nil {
				continue
			}
			if criticalWorkload(svc) {
				fmt.Fprintf(oc.stdout, "BACKUP SKIP %s (registry-critical: --live only)\n", name)
				continue
			}
			services = append(services, name)
		}
	}
	if len(services) == 0 {
		return opError(fmt.Errorf("no services with storage to back up"))
	}
	names := services
	sort.Strings(names)

	runner, cleanup, err := newKubectlRunner(oc.site, oc.p.Root)
	if err != nil {
		return opError(err)
	}
	defer cleanup()

	// crash-safety sweep FIRST: resume anything a dead process left at 0
	if n := sweepQuiesced(oc, runner); n > 0 {
		fmt.Fprintf(oc.stdout, "RECOVERED %d service(s) left quiesced by a dead run\n", n)
	}
	// R2 creds reach the Jobs as a k8s Secret (synced from the secrets home)
	if err := ensureR2EnvRepository(oc, cfg); err != nil {
		return opError(err)
	}
	if err := labEnsureSecret(runner, oc.site.Namespace, "fleet-r2-creds",
		filepath.Join(oc.site.secretsDir(oc.p.Root), "r2.env")); err != nil {
		return opError(err)
	}

	done := 0
	for _, name := range names {
		svc := oc.lv.LabServices()[name]
		if asMap(svc["storage"]) == nil {
			fmt.Fprintf(oc.stdout, "BACKUP SKIP %s (no storage)\n", name)
			continue
		}
		if err := backupOne(oc, runner, name, cfg, live); err != nil {
			return opError(err)
		}
		done++
	}

	// retention + secrets home (host side)
	forgetArgs := append([]string{"forget", "--prune"}, strings.Fields(cfg.Retention)...)
	if _, err := hostRestic(creds, cfg, forgetArgs...); err != nil {
		return opError(fmt.Errorf("retention (restic forget): %v", err))
	}
	if _, err := hostRestic(creds, cfg, "backup",
		oc.site.secretsDir(oc.p.Root), "--tag", "kind=secrets", "--tag", "site="+oc.site.Name); err != nil {
		return opError(fmt.Errorf("secrets backup: %v", err))
	}

	AppendJournal(oc.p.Journal, fmt.Sprintf(
		"# backup wo=%s site=%s services=%s live=%v", os.Getenv("FLEET_WO"), oc.site.Name, strings.Join(names, ","), live))
	fmt.Fprintf(oc.stdout, "BACKUP OK site=%s services=%d (secrets home included, retention %s)\n",
		oc.site.Name, done, cfg.Retention)
	return 0
}

// criticalWorkload reports registry-marked critical services (never
// quiesced: --live backups only). Registry-driven, not hardcoded.
func criticalWorkload(svc map[string]any) bool {
	return asBool(svc["critical"])
}

// quiesceStatePath is the crash-safety record (WO-20 piece 3): written
// BEFORE scale-0, removed after resume. Any mutating ops invocation
// sweeps it first — a dead process can never leave a service down.
func quiesceStatePath(site Site, root string) string {
	return filepath.Join(site.LabRootAbs(root), "state", "quiesce_state.json")
}

// sweepQuiesced resumes any deployment left scaled down by a dead
// backup/restore process. Returns the number resumed.
func sweepQuiesced(oc *opsContext, r *kubectlRunner) int {
	raw, err := os.ReadFile(quiesceStatePath(oc.site, oc.p.Root))
	if err != nil {
		return 0
	}
	var entries []struct {
		Service  string `json:"service"`
		Replicas int    `json:"replicas"`
	}
	if json.Unmarshal(raw, &entries) != nil {
		return 0
	}
	resumed := 0
	for _, e := range entries {
		if out, rc := r.Run("-n", oc.site.Namespace, "scale",
			"deployment/"+e.Service, fmt.Sprintf("--replicas=%d", e.Replicas), "--timeout=120s"); rc != 0 {
			fmt.Fprintf(oc.stdout, "RECOVER WARN %s: %s\n", e.Service, strings.TrimSpace(out))
			continue
		}
		AppendJournal(oc.p.Journal, fmt.Sprintf(
			"# quiesce-recovered site=%s service=%s replicas=%d", oc.site.Name, e.Service, e.Replicas))
		resumed++
	}
	os.Remove(quiesceStatePath(oc.site, oc.p.Root))
	return resumed
}

func recordQuiesce(oc *opsContext, name string, replicas int) error {
	raw, _ := os.ReadFile(quiesceStatePath(oc.site, oc.p.Root))
	var entries []struct {
		Service  string `json:"service"`
		Replicas int    `json:"replicas"`
	}
	json.Unmarshal(raw, &entries)
	entries = append(entries, struct {
		Service  string `json:"service"`
		Replicas int    `json:"replicas"`
	}{name, replicas})
	raw, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(quiesceStatePath(oc.site, oc.p.Root)), 0o755); err != nil {
		return err
	}
	return os.WriteFile(quiesceStatePath(oc.site, oc.p.Root), raw, 0o644)
}

func clearQuiesce(oc *opsContext, name string) {
	raw, _ := os.ReadFile(quiesceStatePath(oc.site, oc.p.Root))
	var entries []struct {
		Service  string `json:"service"`
		Replicas int    `json:"replicas"`
	}
	json.Unmarshal(raw, &entries)
	var keep []struct {
		Service  string `json:"service"`
		Replicas int    `json:"replicas"`
	}
	for _, e := range entries {
		if e.Service != name {
			keep = append(keep, e)
		}
	}
	if len(keep) == 0 {
		os.Remove(quiesceStatePath(oc.site, oc.p.Root))
		return
	}
	raw, _ = json.Marshal(keep)
	os.WriteFile(quiesceStatePath(oc.site, oc.p.Root), raw, 0o644)
}

// backupOne: quiesce → restic backup Job → resume. Critical services
// (registry critical: true) refuse scale-down — --live only, ever.
func backupOne(oc *opsContext, r *kubectlRunner, name string, cfg *backupConfig, live bool) error {
	ns := oc.site.Namespace
	svc := oc.lv.LabServices()[name]
	replicas := 1
	if !live {
		if criticalWorkload(svc) {
			return fmt.Errorf("%s is registry-critical: quiesce refused — use --live (hot snapshot, no downtime)", name)
		}
		replicas = deploymentReplicas(r, ns, name)
		if replicas > 0 {
			if err := recordQuiesce(oc, name, replicas); err != nil {
				return err
			}
			if out, rc := r.Run("-n", ns, "scale", "deployment/"+name, "--replicas=0", "--timeout=60s"); rc != 0 {
				clearQuiesce(oc, name)
				return fmt.Errorf("quiesce %s: %s", name, strings.TrimSpace(out))
			}
			defer func() {
				if out, rc := r.Run("-n", ns, "scale", "deployment/"+name,
					fmt.Sprintf("--replicas=%d", replicas), "--timeout=120s"); rc != 0 {
					fmt.Fprintf(oc.stdout, "BACKUP WARN %s resume failed: %s\n", name, strings.TrimSpace(out))
				}
				clearQuiesce(oc, name)
			}()
		}
	}
	jobName := fmt.Sprintf("backup-%s-%s", name, time.Now().UTC().Format("20060102-150405"))
	docs := renderResticJob(ns, jobName, name, "backup",
		[]string{"backup", "/srv/backup", "--tag", "service=" + name, "--tag", "site=" + oc.site.Name})
	if err := applyLabDocs(r, docs); err != nil {
		return fmt.Errorf("backup job %s: %v", jobName, err)
	}
	if err := labWaitJob(r, ns, jobName, "600s"); err != nil {
		return fmt.Errorf("backup job %s: %v", jobName, err)
	}
	fmt.Fprintf(oc.stdout, "BACKUP OK %s (job %s)\n", name, jobName)
	return nil
}

// --- ops restore -----------------------------------------------------------

func opsRestore(oc *opsContext, args []string) int {
	snapshot := "latest"
	plan := false
	var services []string
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--plan":
			plan = true
		case args[i] == "--snapshot":
			if i+1 >= len(args) {
				return opError(fmt.Errorf("--snapshot requires latest|<id>"))
			}
			snapshot = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--snapshot="):
			snapshot = strings.TrimPrefix(args[i], "--snapshot=")
		default:
			if strings.HasPrefix(args[i], "-") {
				return opError(fmt.Errorf("unknown flag %q for ops restore", args[i]))
			}
			services = append(services, args[i])
		}
	}
	cfg := loadBackupConfig(oc.lv)
	if cfg == nil {
		return opError(fmt.Errorf("site '%s' has no backup: section", oc.site.Name))
	}
	names := services
	if len(names) == 0 {
		return opError(fmt.Errorf("usage: fleet ops restore --site S <service>... [--snapshot latest|ID] [--plan]"))
	}
	runner, cleanup, err := newKubectlRunner(oc.site, oc.p.Root)
	if err != nil {
		return opError(err)
	}
	defer cleanup()
	if err := ensureR2EnvRepository(oc, cfg); err != nil {
		return opError(err)
	}
	sweepQuiesced(oc, runner)
	if err := labEnsureSecret(runner, oc.site.Namespace, "fleet-r2-creds",
		filepath.Join(oc.site.secretsDir(oc.p.Root), "r2.env")); err != nil {
		return opError(err)
	}
	for _, name := range names {
		if asMap(oc.lv.LabServices()[name]["storage"]) == nil {
			return opError(fmt.Errorf("service '%s' has no storage to restore", name))
		}
		if plan {
			fmt.Fprintf(oc.stdout, "RESTORE PLAN %s snapshot=%s: scale 0 -> restore Job (PVC <name>-data) -> scale back -> ops verify %s\n",
				name, snapshot, name)
			continue
		}
		if err := restoreOne(oc, runner, name, snapshot); err != nil {
			return opError(err)
		}
		AppendJournal(oc.p.Journal, fmt.Sprintf(
			"# restore wo=%s site=%s service=%s snapshot=%s", os.Getenv("FLEET_WO"), oc.site.Name, name, snapshot))
		fmt.Fprintf(oc.stdout, "RESTORED %s snapshot=%s\n", name, snapshot)
	}
	return 0
}

func restoreOne(oc *opsContext, r *kubectlRunner, name, snapshot string) error {
	ns := oc.site.Namespace
	if criticalWorkload(oc.lv.LabServices()[name]) {
		return fmt.Errorf("%s is registry-critical: quiesce refused — restore requires a maintenance window agreed with the owner", name)
	}
	replicas := deploymentReplicas(r, ns, name)
	if replicas > 0 {
		if err := recordQuiesce(oc, name, replicas); err != nil {
			return err
		}
		if out, rc := r.Run("-n", ns, "scale", "deployment/"+name, "--replicas=0", "--timeout=60s"); rc != 0 {
			clearQuiesce(oc, name)
			return fmt.Errorf("quiesce %s: %s", name, strings.TrimSpace(out))
		}
		defer func() {
			if out, rc := r.Run("-n", ns, "scale", "deployment/"+name,
				fmt.Sprintf("--replicas=%d", replicas), "--timeout=120s"); rc != 0 {
				fmt.Fprintf(oc.stdout, "RESTORE WARN %s resume failed: %s\n", name, strings.TrimSpace(out))
			}
			clearQuiesce(oc, name)
		}()
	}
	jobName := fmt.Sprintf("restore-%s-%s", name, time.Now().UTC().Format("20060102-150405"))
	docs := renderResticJob(ns, jobName, name, "restore",
		[]string{"restore", snapshot, "--tag", "service=" + name, "--target", "/"})
	if err := applyLabDocs(r, docs); err != nil {
		return fmt.Errorf("restore job %s: %v", jobName, err)
	}
	if err := labWaitJob(r, ns, jobName, "900s"); err != nil {
		return fmt.Errorf("restore job %s: %v", jobName, err)
	}
	return nil
}

// ensureR2EnvRepository composes RESTIC_REPOSITORY (s3:endpoint/bucket/repo)
// into r2.env if missing — the in-cluster Jobs read it from the
// fleet-r2-creds Secret, the host reader uses the composed value too.
func ensureR2EnvRepository(oc *opsContext, cfg *backupConfig) error {
	r2Path := filepath.Join(oc.site.secretsDir(oc.p.Root), "r2.env")
	raw, err := os.ReadFile(r2Path)
	if err != nil {
		return err
	}
	repo := fmt.Sprintf("s3:%s/%s/%s", cfg.Endpoint, cfg.Bucket, cfg.Repo)
	for _, ln := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "RESTIC_REPOSITORY=") {
			return nil
		}
	}
	f, err := os.OpenFile(r2Path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString("RESTIC_REPOSITORY=" + repo + "\n")
	return err
}

// opsRestoreSecrets (WO-20): fresh-box bootstrap — restore the site's
// secrets home from the backup repo (host-side restic; kind=secrets).
// Refuses to overwrite existing files without --force.
func opsRestoreSecrets(oc *opsContext, args []string) int {
	snapshot := "latest"
	force := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--force":
			force = true
		case "--snapshot":
			if i+1 >= len(args) {
				return opError(fmt.Errorf("--snapshot requires latest|<id>"))
			}
			snapshot = args[i+1]
			i++
		default:
			return opError(fmt.Errorf("unknown argument %q for ops restore secrets", args[i]))
		}
	}
	cfg := loadBackupConfig(oc.lv)
	if cfg == nil {
		return opError(fmt.Errorf("site '%s' has no backup: section", oc.site.Name))
	}
	creds, err := loadR2Creds(oc.site.secretsDir(oc.p.Root))
	if err != nil {
		return opError(err)
	}
	dest := oc.site.secretsDir(oc.p.Root)
	staging, err := os.MkdirTemp("", "fleet-secrets-restore-*")
	if err != nil {
		return opError(err)
	}
	defer os.RemoveAll(staging)
	if _, err := hostRestic(creds, cfg, "restore", snapshot,
		"--tag", "kind=secrets", "--target", staging); err != nil {
		return opError(fmt.Errorf("secrets restore: %v", err))
	}
	// restic recreates absolute paths under the target: find the .env files
	var envFiles []string
	filepath.Walk(staging, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(p, ".env") {
			envFiles = append(envFiles, p)
		}
		return nil
	})
	if len(envFiles) == 0 {
		return opError(fmt.Errorf("no .env files in secrets snapshot %s", snapshot))
	}
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return opError(err)
	}
	restored := 0
	for _, src := range envFiles {
		dst := filepath.Join(dest, filepath.Base(src))
		if _, err := os.Stat(dst); err == nil && !force {
			return opError(fmt.Errorf("%s exists — pass --force to overwrite (r2.env keeps your live keys unless forced)", dst))
		}
		raw, err := os.ReadFile(src)
		if err != nil {
			return opError(err)
		}
		if err := os.WriteFile(dst, raw, 0o600); err != nil {
			return opError(err)
		}
		restored++
	}
	AppendJournal(oc.p.Journal, fmt.Sprintf(
		"# restore-secrets wo=%s site=%s snapshot=%s files=%d",
		os.Getenv("FLEET_WO"), oc.site.Name, snapshot, restored))
	fmt.Fprintf(oc.stdout, "SECRETS RESTORED site=%s snapshot=%s files=%d\n", oc.site.Name, snapshot, restored)
	return 0
}

// --- schedule ---------------------------------------------------------------

func opsBackupSchedule(oc *opsContext, cfg *backupConfig, schedule string) int {
	cron := map[string]string{"nightly": "0 4 * * *", "weekly": "0 4 * * 0", "none": ""}[schedule]
	if _, ok := map[string]bool{"nightly": true, "weekly": true, "none": true}[schedule]; !ok {
		return opError(fmt.Errorf("--schedule must be nightly|weekly|none"))
	}
	runner, cleanup, err := newKubectlRunner(oc.site, oc.p.Root)
	if err != nil {
		return opError(err)
	}
	defer cleanup()
	if err := ensureR2EnvRepository(oc, cfg); err != nil {
		return opError(err)
	}
	if err := labEnsureSecret(runner, oc.site.Namespace, "fleet-r2-creds",
		filepath.Join(oc.site.secretsDir(oc.p.Root), "r2.env")); err != nil {
		return opError(err)
	}
	for _, name := range oc.lv.LabServiceNames() {
		if asMap(oc.lv.LabServices()[name]["storage"]) == nil {
			continue
		}
		jobName := "backup-" + name
		if cron == "" {
			if out, rc := runner.Run("-n", oc.site.Namespace, "delete", "cronjob", jobName, "--ignore-not-found"); rc != 0 {
				return opError(fmt.Errorf("delete cronjob %s: %s", jobName, strings.TrimSpace(out)))
			}
			continue
		}
		docs := renderResticCron(oc.site.Namespace, jobName, name, cron,
			[]string{"backup", "/srv/backup", "--tag", "service=" + name, "--tag", "site=" + oc.site.Name})
		if err := applyLabDocs(runner, docs); err != nil {
			return opError(fmt.Errorf("cronjob %s: %v", jobName, err))
		}
	}
	if err := labSetBackupSchedule(oc, schedule); err != nil {
		return opError(err)
	}
	AppendJournal(oc.p.Journal, fmt.Sprintf(
		"# backup-schedule wo=%s site=%s schedule=%s", os.Getenv("FLEET_WO"), oc.site.Name, schedule))
	fmt.Fprintf(oc.stdout, "SCHEDULE SET site=%s schedule=%s (retention %s)\n", oc.site.Name, schedule, cfg.Retention)
	return 0
}

func labSetBackupSchedule(oc *opsContext, schedule string) error {
	path := filepath.Join(oc.labRoot, "config", "registry.yaml")
	lines, err := readLines(path)
	if err != nil {
		return err
	}
	var out []string
	inBackup := false
	replaced := false
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		if t == "backup:" {
			inBackup = true
		} else if inBackup && t != "" && !strings.HasPrefix(ln, "#") && !strings.HasPrefix(ln, " ") {
			inBackup = false
		}
		if inBackup && strings.HasPrefix(t, "schedule:") {
			out = append(out, ln[:len(ln)-len(t)]+"schedule: "+schedule)
			replaced = true
			continue
		}
		out = append(out, ln)
	}
	if !replaced {
		return fmt.Errorf("registry has no backup.schedule line to anchor")
	}
	text := strings.Join(out, "\n")
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	tmp := path + ".fleet.tmp"
	if err := os.WriteFile(tmp, []byte(text), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	if _, err := LoadLabView(oc.site, oc.p.Root); err != nil {
		return fmt.Errorf("registry edit failed validation: %v", err)
	}
	return nil
}

// --- render helpers ---------------------------------------------------------

// renderResticJob renders a restic Job: official image, PVC <name>-data
// mounted at /srv/backup, R2 creds from the fleet-r2-creds Secret,
// command = plain restic args (the image has no shell — by design).
func renderResticJob(ns, jobName, svcName, kind string, resticArgs []string) []any {
	labels := map[string]any{"app": jobName, "fleet-role": "backup", "fleet-service": svcName}
	env := []any{
		map[string]any{"name": "RESTIC_REPOSITORY", "valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": "fleet-r2-creds", "key": "RESTIC_REPOSITORY"}}},
		map[string]any{"name": "RESTIC_PASSWORD", "valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": "fleet-r2-creds", "key": "RESTIC_PASSWORD"}}},
		map[string]any{"name": "AWS_ACCESS_KEY_ID", "valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": "fleet-r2-creds", "key": "R2_ACCESS_KEY_ID"}}},
		map[string]any{"name": "AWS_SECRET_ACCESS_KEY", "valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": "fleet-r2-creds", "key": "R2_SECRET_ACCESS_KEY"}}},
	}
	ctr := map[string]any{
		"name":            kind,
		"image":           resticImage,
		"imagePullPolicy": "IfNotPresent",
		"command":         []any{"restic"},
		"args":            toAnySlice(resticArgs),
		"env":             env,
		"volumeMounts":    []any{map[string]any{"name": "data", "mountPath": "/srv/backup"}},
		"resources": map[string]any{
			"requests": map[string]any{"memory": "128Mi", "cpu": "100m"},
			"limits":   map[string]any{"memory": "512Mi"},
		},
	}
	pod := map[string]any{
		"containers":  []any{ctr},
		"restartPolicy": "Never",
		"volumes":     []any{map[string]any{"name": "data", "persistentVolumeClaim": map[string]any{"claimName": svcName + "-data"}}},
	}
	job := map[string]any{
		"apiVersion": "batch/v1",
		"kind":       "Job",
		"metadata":   map[string]any{"name": jobName, "namespace": ns, "labels": labels},
		"spec": map[string]any{
			"backoffLimit": 0,
			"template":     map[string]any{"metadata": map[string]any{"labels": labels}, "spec": pod},
		},
	}
	return []any{job}
}

func renderResticCron(ns, jobName, svcName, cron string, resticArgs []string) []any {
	job := renderResticJob(ns, jobName, svcName, "backup", resticArgs)[0]
	cj := map[string]any{
		"apiVersion": "batch/v1",
		"kind":       "CronJob",
		"metadata":   map[string]any{"name": jobName, "namespace": ns},
		"spec": map[string]any{
			"schedule":          cron,
			"concurrencyPolicy": "Forbid",
			"jobTemplate":       map[string]any{"spec": map[string]any{"template": job.(map[string]any)["spec"].(map[string]any)["template"]}},
		},
	}
	return []any{cj}
}

func toAnySlice(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// deploymentReplicas returns the deployment's desired replicas (0 when
// it does not exist — backup/restore still proceed for the PVC).
func deploymentReplicas(r *kubectlRunner, ns, name string) int {
	out, rc := r.Run("-n", ns, "get", "deployment", name,
		"-o", "jsonpath={.spec.replicas}")
	if rc != 0 {
		return 0
	}
	n, _ := atoiStrict(strings.TrimSpace(out))
	return n
}
