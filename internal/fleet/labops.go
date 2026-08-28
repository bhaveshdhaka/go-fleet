package fleet

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// Lab mutation primitives (WO-8): sos-lab-compatible state writers
// (byte-format + locking identical to labctl/state.py), surgical registry
// edits validated by re-parse, kubectl helpers over the explicit runner,
// and Cloudflare write calls. Verb wiring lives in opsmutate.go.

// --- lab state (labctl/state.py parity) ---------------------------------

func labNodeName() string {
	if n := os.Getenv("SOS_LAB_NODE"); n != "" {
		return n
	}
	return "hk-03-dev"
}

// labStateWrite replaces state/<file> with data in labctl's exact byte
// format: json.dump(indent=2, sort_keys=True) + trailing newline, tmp file
// in the same directory + rename, exclusive flock on state/.<lock>.lock —
// the same lock files labctl holds.
func labStateWrite(labRoot, file string, data map[string]any) error {
	dir := filepath.Join(labRoot, "state")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	lock := strings.TrimSuffix(file, ".json")
	lf, err := os.OpenFile(filepath.Join(dir, "."+lock+".lock"), os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer lf.Close()
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lf.Fd()), syscall.LOCK_UN)

	tmp := filepath.Join(dir, file+".tmp")
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(pyJSONIndent(data) + "\n"); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, file))
}

func labNowTS(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05Z")
}

// recordLabDeploy mirrors state.record_deploy.
func recordLabDeploy(labRoot, service, tag, image, gitSha string, now time.Time) (map[string]any, error) {
	entry := map[string]any{
		"tag":         tag,
		"image":       image,
		"deployed_at": labNowTS(now),
		"node":        labNodeName(),
	}
	if gitSha != "" {
		entry["git_sha"] = gitSha
	}
	data, err := loadJSONFile(filepath.Join(labRoot, "state", "deployed.json"))
	if err != nil {
		return nil, err
	}
	data[service] = entry
	if err := labStateWrite(labRoot, "deployed.json", data); err != nil {
		return nil, err
	}
	return entry, nil
}

// recordLabBuild mirrors state.record_build (git_sha key ALWAYS present,
// null when unknown — python writes it unconditionally).
func recordLabBuild(labRoot, service, tag, gitSha string, now time.Time) error {
	entry := map[string]any{
		"tag":      tag,
		"git_sha":  nil,
		"built_at": labNowTS(now),
	}
	if gitSha != "" {
		entry["git_sha"] = gitSha
	}
	data, err := loadJSONFile(filepath.Join(labRoot, "state", "builds.json"))
	if err != nil {
		return err
	}
	data[service] = entry
	return labStateWrite(labRoot, "builds.json", data)
}

// recordLabRollback mirrors state.record_rollback.
func recordLabRollback(labRoot, service, tag, image string, now time.Time) (map[string]any, error) {
	entry := map[string]any{
		"tag":         tag,
		"image":       image,
		"deployed_at": labNowTS(now),
		"node":        labNodeName(),
		"rolled_back": true,
	}
	data, err := loadJSONFile(filepath.Join(labRoot, "state", "deployed.json"))
	if err != nil {
		return nil, err
	}
	data[service] = entry
	if err := labStateWrite(labRoot, "deployed.json", data); err != nil {
		return nil, err
	}
	return entry, nil
}

// removeLabStateEntry deletes one service's state entry (fleet extension,
// journaled deviation: ./lab remove leaves doctor red forever because no
// tool cleans state).
func removeLabStateEntry(labRoot, file, service string) error {
	data, err := loadJSONFile(filepath.Join(labRoot, "state", file))
	if err != nil {
		return err
	}
	if _, ok := data[service]; !ok {
		return nil
	}
	delete(data, service)
	return labStateWrite(labRoot, file, data)
}

// --- registry anchored edits --------------------------------------------

// labServiceBlock locates the config/registry.yaml line range of one
// service block: from the "  <name>:" line up to (excluding) the next
// non-empty, non-comment line with indent <= 2. Trailing comment lines
// attached to the block are included.
func labServiceBlock(lines []string, name string) (start, end int, ok bool) {
	for i, ln := range lines {
		if strings.TrimRight(ln, " ") == "  "+name+":" {
			start = i
			break
		}
	}
	if start < 0 {
		return 0, 0, false
	}
	end = len(lines)
	for i := start + 1; i < len(lines); i++ {
		t := strings.TrimLeft(lines[i], " ")
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if len(lines[i])-len(t) <= 2 {
			end = i
			break
		}
	}
	// swallow trailing comment lines directly above the next block/section
	for end-1 > start {
		t := strings.TrimLeft(lines[end-1], " ")
		if strings.HasPrefix(t, "#") {
			end--
			continue
		}
		break
	}
	return start, end, true
}

// labSetServiceEnabled flips one service's `enabled:` line via a surgical
// anchored edit, then validates the whole file by re-parsing.
func labSetServiceEnabled(labRoot, name string, enabled bool) error {
	path := filepath.Join(labRoot, "config", "registry.yaml")
	lines, err := readLines(path)
	if err != nil {
		return err
	}
	start, end, ok := labServiceBlock(lines, name)
	if !ok {
		return fmt.Errorf("service '%s' is not registered in %s", name, path)
	}
	want := "    enabled: false"
	if enabled {
		want = "    enabled: true"
	}
	found := false
	for i := start; i < end; i++ {
		if strings.HasPrefix(strings.TrimLeft(lines[i], " \t"), "enabled:") &&
			len(lines[i])-len(strings.TrimLeft(lines[i], " \t")) == 4 {
			lines[i] = want
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("service '%s': no 'enabled:' line to flip — add 'enabled: false' to its registry block first", name)
	}
	return labRegistryRewrite(labRoot, lines, func(reg map[string]any) error {
		svc := asMap(asMap(reg["services"])[name])
		if svc == nil {
			return fmt.Errorf("service '%s' missing after edit", name)
		}
		if asBool(svc["enabled"]) != enabled {
			return fmt.Errorf("service '%s': enabled flip did not take effect", name)
		}
		return nil
	})
}

// labRemoveService deletes one service's registry block (surgical line
// deletion), then validates the file by re-parsing.
func labRemoveService(labRoot, name string) error {
	path := filepath.Join(labRoot, "config", "registry.yaml")
	lines, err := readLines(path)
	if err != nil {
		return err
	}
	start, end, ok := labServiceBlock(lines, name)
	if !ok {
		return fmt.Errorf("service '%s' is not registered in %s", name, path)
	}
	out := append(append([]string{}, lines[:start]...), lines[end:]...)
	return labRegistryRewrite(labRoot, out, func(reg map[string]any) error {
		if asMap(asMap(reg["services"])[name]) != nil {
			return fmt.Errorf("service '%s' still present after removal", name)
		}
		return nil
	})
}

// labRegistryRewrite writes the edited registry lines atomically and
// re-validates: parse with the bounded parser + the WO-7 registry
// validation, then run the caller's assertion.
func labRegistryRewrite(labRoot string, lines []string, assert func(map[string]any) error) error {
	text := strings.Join(lines, "\n")
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	top, err := parseMiniYAML(text)
	if err != nil {
		return fmt.Errorf("registry edit produced invalid yaml: %v", err)
	}
	reg := asMap(top)
	if reg == nil {
		return fmt.Errorf("registry edit produced a non-mapping document")
	}
	if err := validateLabRegistry(reg); err != nil {
		return fmt.Errorf("registry edit failed validation: %v", err)
	}
	if err := assert(reg); err != nil {
		return err
	}
	path := filepath.Join(labRoot, "config", "registry.yaml")
	tmp := path + ".fleet.tmp"
	if err := os.WriteFile(tmp, []byte(text), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// --- kubectl helpers over the explicit runner ---------------------------

var labKubectlTimeout = 300 * time.Second

// runWithInput execs kubectl with stdin (apply -f -) and separated output.
func (r *kubectlRunner) runWithInput(args []string, input string) (string, string, int) {
	return r.runTimeout(args, input, labKubectlTimeout)
}

func (r *kubectlRunner) runTimeout(args []string, input string, timeout time.Duration) (string, string, int) {
	cmd := exec.Command("kubectl", args...)
	cmd.Env = []string{"KUBECONFIG=" + r.kubeconfig, "HOME=" + r.homeDir()}
	cmd.Stdin = strings.NewReader(input)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if timeout > 0 {
		t := time.AfterFunc(timeout, func() { _ = cmd.Process.Kill() })
		defer t.Stop()
	}
	err := cmd.Run()
	rc := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			rc = ee.ExitCode()
		} else {
			return "", fmt.Sprintf("cannot execute kubectl: %v", err), 127
		}
	}
	return stdout.String(), stderr.String(), rc
}

// kubectlErr mirrors k8s.KubectlError's message shape.
func kubectlErr(args []string, stdout, stderr string, rc int) error {
	detail := strings.TrimSpace(stderr)
	if detail == "" {
		detail = strings.TrimSpace(stdout)
	}
	return fmt.Errorf("kubectl %s failed:\n%s", strings.Join(args, " "), detail)
}

// applyLabDocs applies each document as canonical JSON — one kubectl call
// per document, output swallowed exactly like labctl (apply_documents
// never prints). Applied object graphs are the parity contract.
func applyLabDocs(r *kubectlRunner, docs []any) error {
	for _, doc := range docs {
		raw, err := jsonMarshal(doc)
		if err != nil {
			return err
		}
		out, errOut, rc := r.runWithInput([]string{"apply", "-f", "-"}, string(raw)+"\n")
		if rc != 0 {
			return kubectlErr([]string{"apply", "-f", "-"}, out, errOut, rc)
		}
	}
	return nil
}

// labRolloutStatus mirrors k8s.rollout_status (output swallowed — labctl
// captures it silently).
func labRolloutStatus(r *kubectlRunner, ns, name, timeout string) error {
	args := []string{"-n", ns, "rollout", "status", "deployment/" + name, "--timeout=" + timeout}
	out, errOut, rc := r.runTimeout(args, "", labKubectlTimeout)
	if rc != 0 {
		return kubectlErr(args, out, errOut, rc)
	}
	return nil
}

// labRolloutUndo / labRolloutRestart mirror k8s.rollout_undo and the
// monitor's rollout restart (silent).
func labRolloutUndo(r *kubectlRunner, ns, name string) error {
	args := []string{"-n", ns, "rollout", "undo", "deployment/" + name}
	out, errOut, rc := r.runTimeout(args, "", labKubectlTimeout)
	if rc != 0 {
		return kubectlErr(args, out, errOut, rc)
	}
	return nil
}

func labRolloutRestart(r *kubectlRunner, ns, name string) error {
	args := []string{"-n", ns, "rollout", "restart", "deployment/" + name}
	out, errOut, rc := r.runTimeout(args, "", labKubectlTimeout)
	if rc != 0 {
		return kubectlErr(args, out, errOut, rc)
	}
	return nil
}

// labEnsureSecret mirrors k8s.ensure_secret: kubectl renders the secret
// (dry-run client) from the env file — values flow kubectl-side only —
// then the rendered document is applied.
func labEnsureSecret(r *kubectlRunner, ns, name, envFile string) error {
	args := []string{"-n", ns, "create", "secret", "generic", name,
		"--from-env-file=" + envFile, "--dry-run=client", "-o", "yaml"}
	out, errOut, rc := r.runTimeout(args, "", labKubectlTimeout)
	if rc != 0 {
		return kubectlErr(args, out, errOut, rc)
	}
	aout, aerr, arc := r.runWithInput([]string{"apply", "-f", "-"}, out)
	if arc != 0 {
		return kubectlErr([]string{"apply", "-f", "-"}, aout, aerr, arc)
	}
	return nil
}

// labWaitJob mirrors k8s.wait_job.
func labWaitJob(r *kubectlRunner, ns, name, timeout string) error {
	args := []string{"-n", ns, "wait", "--for=condition=complete", "job/" + name, "--timeout=" + timeout}
	out, errOut, rc := r.runTimeout(args, "", labKubectlTimeout)
	if rc != 0 {
		return kubectlErr(args, out, errOut, rc)
	}
	return nil
}

// labDescribeTail mirrors the build-failure describe (last 4000 chars).
func labDescribeTail(r *kubectlRunner, ns, pod string) string {
	args := []string{"-n", ns, "describe", "pod", pod}
	out, errOut, rc := r.runTimeout(args, "", labKubectlTimeout)
	text := out
	if text == "" {
		text = errOut
	}
	if rc != 0 && text == "" {
		text = fmt.Sprintf("describe failed rc=%d", rc)
	}
	if len(text) > 4000 {
		return text[len(text)-4000:]
	}
	return text
}

// labPreviousImage mirrors k8s.previous_image: the image of the ReplicaSet
// one revision behind current ("" when unknown).
func labPreviousImage(r *kubectlRunner, ns, name string) string {
	args := []string{"-n", ns, "get", "rs", "-l", "app=" + name, "-o", "json"}
	out, _, rc := r.runTimeout(args, "", labKubectlTimeout)
	if rc != 0 {
		return ""
	}
	var rs struct {
		Items []struct {
			Metadata struct {
				Annotations map[string]string `json:"annotations"`
			} `json:"metadata"`
			Spec struct {
				Template struct {
					Spec struct {
						Containers []struct {
							Image string `json:"image"`
						} `json:"containers"`
					} `json:"spec"`
				} `json:"template"`
			} `json:"spec"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &rs); err != nil {
		return ""
	}
	type rev struct {
		n   int
		img string
	}
	var revs []rev
	for _, it := range rs.Items {
		n := 0
		fmt.Sscanf(it.Metadata.Annotations["deployment.kubernetes.io/revision"], "%d", &n)
		if len(it.Spec.Template.Spec.Containers) == 0 {
			continue
		}
		revs = append(revs, rev{n, it.Spec.Template.Spec.Containers[0].Image})
	}
	if len(revs) < 2 {
		return ""
	}
	sort.Slice(revs, func(i, j int) bool { return revs[i].n < revs[j].n })
	return revs[len(revs)-2].img
}

// labBuildPodForJob mirrors cli._wait_for_build_pod (90s deadline, 2s poll).
func labBuildPodForJob(r *kubectlRunner, ns, jobName string) (string, error) {
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		args := []string{"-n", ns, "get", "pods", "-l", "app=" + jobName, "-o", "json"}
		out, _, rc := r.runTimeout(args, "", labKubectlTimeout)
		if rc == 0 {
			var pods struct {
				Items []struct {
					Metadata struct {
						Name string `json:"name"`
					} `json:"metadata"`
				} `json:"items"`
			}
			if err := json.Unmarshal([]byte(out), &pods); err == nil && len(pods.Items) > 0 {
				return pods.Items[0].Metadata.Name, nil
			}
		}
		time.Sleep(2 * time.Second)
	}
	return "", fmt.Errorf("build pod for job %s never appeared", jobName)
}

// labStream runs kubectl streaming its stdout+stderr merged to os.Stdout
// (logs -f) and returns the command's exit state.
func labStream(r *kubectlRunner, args ...string) error {
	return r.streamToStdout(args)
}

// --- build path helpers (cli._resolve_repo_path + git checks) -----------

func labResolveRepoPath(labRoot, repo string) (string, error) {
	if strings.HasPrefix(repo, "http://") || strings.HasPrefix(repo, "https://") ||
		strings.HasPrefix(repo, "git@") {
		return "", fmt.Errorf(
			"remote git fetch is unreliable on this network — clone '%s' into /workspace/<name> and set repo: <directory-name>", repo)
	}
	name := strings.TrimPrefix(repo, "../")
	name = strings.TrimPrefix(name, "/")
	path := filepath.Join(filepath.Dir(labRoot), name)
	st, err := os.Stat(path)
	if err != nil || !st.IsDir() {
		return "", fmt.Errorf("repo directory not found: %s", path)
	}
	return path, nil
}

func labGitSha(path string) string {
	out, err := exec.Command("git", "-C", path, "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func labGitDirty(path string) bool {
	out, err := exec.Command("git", "-C", path, "status", "--porcelain").Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

// --- cloudflare writes (labctl/cloudflare.py parity) --------------------

func cloudflareAPIBody(token, method, path string, body any) (json.RawMessage, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return cloudflareAPIRaw(token, method, path, payload)
}

// EnsureCname mirrors cloudflare.ensure_cname: (status, message) exactly.
func EnsureCname(token, zoneID, host, target string, apply bool) (string, string, error) {
	existing, err := GetCnames(token, zoneID, host)
	if err != nil {
		return "", "", err
	}
	if len(existing) == 0 {
		if !apply {
			return "missing", fmt.Sprintf("%s: CNAME absent (want -> %s)", host, target), nil
		}
		if _, err := cloudflareAPIBody(token, "POST", "/zones/"+zoneID+"/dns_records",
			map[string]any{"type": "CNAME", "name": host, "content": target, "proxied": true}); err != nil {
			return "", "", err
		}
		return "created", fmt.Sprintf("%s: CNAME created -> %s", host, target), nil
	}
	rec := existing[0]
	recID := asString(rec["id"])
	content := asString(rec["content"])
	if content != target {
		if !apply {
			return "drift", fmt.Sprintf("%s: CNAME -> %s (want %s)", host, content, target), nil
		}
		if _, err := cloudflareAPIBody(token, "PUT", "/zones/"+zoneID+"/dns_records/"+recID,
			map[string]any{"type": "CNAME", "name": host, "content": target, "proxied": true}); err != nil {
			return "", "", err
		}
		return "fixed", fmt.Sprintf("%s: CNAME retargeted %s -> %s", host, content, target), nil
	}
	return "ok", fmt.Sprintf("%s: ok", host), nil
}

// PutTunnelConfig mirrors cloudflare.put_tunnel_config: ingress list plus
// the terminating http_status:404 catch-all.
func PutTunnelConfig(token, accountID, tunnelID string, ingress []any) error {
	full := make([]any, len(ingress))
	copy(full, ingress)
	full = append(full, map[string]any{"service": "http_status:404"})
	body := map[string]any{"config": map[string]any{"ingress": full}}
	_, err := cloudflareAPIBody(token, "PUT",
		"/accounts/"+accountID+"/cfd_tunnel/"+tunnelID+"/configurations", body)
	return err
}
