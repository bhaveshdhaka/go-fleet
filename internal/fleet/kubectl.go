package fleet

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Explicit-credential cluster access (WO-7, AGENTS.md rule 7).
//
// fleet NEVER runs kubectl with an ambient environment. The site entry
// declares its access mode; this runner materializes exactly one kubeconfig
// for the call:
//   - in-cluster: a temp kubeconfig pointing at the pod serviceaccount
//     (tokenFile + certificate-authority file references — the secret bytes
//     are never copied anywhere), requiring KUBERNETES_SERVICE_HOST to be
//     present. No file found -> hard error, never an ambient fallback.
//   - kubeconfig:<path>: that exact file (absolute or lab_root-relative).
//
// The child process env is ONLY KUBECONFIG=<path> — nothing else leaks in
// (asserted by C12d).

const saDir = "/var/run/secrets/kubernetes.io/serviceaccount"

type kubectlRunner struct {
	kubeconfig  string // absolute path
	cleanupDir  string
	ownHome     string
	extraClean  string
	site        Site
	root        string
	crictlTried bool
}

func newKubectlRunner(s Site, root string) (*kubectlRunner, func(), error) {
	r := &kubectlRunner{site: s, root: root}
	switch {
	case s.Access == "in-cluster":
		host := os.Getenv("KUBERNETES_SERVICE_HOST")
		port := os.Getenv("KUBERNETES_SERVICE_PORT_HTTPS")
		if host == "" || port == "" {
			port = os.Getenv("KUBERNETES_SERVICE_PORT")
		}
		if host == "" || port == "" {
			return nil, nil, fmt.Errorf(
				"site %s declares access: in-cluster but no in-cluster environment is present (KUBERNETES_SERVICE_HOST missing) — refusing to fall back to ambient credentials",
				s.Name)
		}
		for _, f := range []string{"token", "ca.crt", "namespace"} {
			if _, err := os.Stat(filepath.Join(saDir, f)); err != nil {
				return nil, nil, fmt.Errorf(
					"site %s declares access: in-cluster but %s is missing — refusing ambient fallback",
					s.Name, filepath.Join(saDir, f))
			}
		}
		dir, err := os.MkdirTemp("", "fleet-kubeconfig-*")
		if err != nil {
			return nil, nil, err
		}
		kc := filepath.Join(dir, "kubeconfig")
		cfg := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: site
  cluster:
    server: https://%s:%s
    certificate-authority: %s
users:
- name: site-sa
  user:
    tokenFile: %s
contexts:
- name: site
  context:
    cluster: site
    user: site-sa
current-context: site
`, host, port, filepath.Join(saDir, "ca.crt"), filepath.Join(saDir, "token"))
		if err := os.WriteFile(kc, []byte(cfg), 0o600); err != nil {
			os.RemoveAll(dir)
			return nil, nil, err
		}
		r.kubeconfig, r.cleanupDir = kc, dir
	case strings.HasPrefix(s.Access, "kubeconfig:"):
		p := s.SiteKubeconfig(root)
		if _, err := os.Stat(p); err != nil {
			return nil, nil, fmt.Errorf("site %s kubeconfig missing: %s", s.Name, p)
		}
		r.kubeconfig = p
	default:
		return nil, nil, fmt.Errorf("site %s has invalid access mode '%s'", s.Name, s.Access)
	}
	cleanup := func() {
		if r.cleanupDir != "" {
			os.RemoveAll(r.cleanupDir)
		}
		if r.extraClean != "" {
			os.RemoveAll(r.extraClean)
		}
	}
	return r, cleanup, nil
}

// Run execs kubectl with the explicit kubeconfig and a minimal child env.
// HOME points into the runner's private temp dir: with no HOME kubectl
// litters a .kube cache into the working directory.
func (r *kubectlRunner) Run(args ...string) (string, int) {
	cmd := exec.Command("kubectl", args...)
	cmd.Env = []string{"KUBECONFIG=" + r.kubeconfig, "HOME=" + r.homeDir()}
	out, err := cmd.CombinedOutput()
	rc := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			rc = ee.ExitCode()
		} else {
			return fmt.Sprintf("kubectl exec failed: %v", err), 127
		}
	}
	return string(out), rc
}

// runJSONText is Run for calls where stdout and stderr must be separated
// (status prints stdout verbatim, or stderr on failure).
func (r *kubectlRunner) runStd(args ...string) (string, string, int) {
	cmd := exec.Command("kubectl", args...)
	cmd.Env = []string{"KUBECONFIG=" + r.kubeconfig, "HOME=" + r.homeDir()}
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	rc := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			rc = ee.ExitCode()
		} else {
			return "", fmt.Sprintf("kubectl exec failed: %v", err), 127
		}
	}
	return stdout.String(), stderr.String(), rc
}

func (r *kubectlRunner) homeDir() string {
	if r.cleanupDir != "" {
		return r.cleanupDir
	}
	if r.ownHome == "" {
		dir, err := os.MkdirTemp("", "fleet-home-*")
		if err != nil {
			return "/nonexistent-home"
		}
		r.ownHome = dir
		r.extraClean = dir
	}
	return r.ownHome
}

// ClusterReachable mirrors k8s.cluster_reachable.
func (r *kubectlRunner) ClusterReachable() bool {
	out, rc := r.Run("get", "nodes", "-o", "name")
	return rc == 0 && strings.TrimSpace(out) != ""
}

// PodRunning mirrors k8s.pod_running: all pods for app=<label> Running.
func (r *kubectlRunner) PodRunning(namespace, label string) bool {
	out, rc := r.Run("-n", namespace, "get", "pods", "-l", "app="+label,
		"-o", `jsonpath={.items[*].status.phase}`)
	if rc != 0 {
		return false
	}
	phases := strings.Fields(out)
	if len(phases) == 0 {
		return false
	}
	for _, p := range phases {
		if p != "Running" {
			return false
		}
	}
	return true
}

// DeploymentImage mirrors k8s.deployment_image: container[0] image or "".
func (r *kubectlRunner) DeploymentImage(namespace, name string) string {
	out, rc := r.Run("-n", namespace, "get", "deployment", name,
		"-o", "jsonpath={.spec.template.spec.containers[0].image}")
	if rc != 0 {
		return ""
	}
	return strings.TrimSpace(out)
}

// NodeReady mirrors the doctor nodes Ready scan.
func (r *kubectlRunner) NodeReady() bool {
	out, rc := r.Run("get", "nodes", "-o", "json")
	if rc != 0 {
		return false
	}
	var nodes struct {
		Items []struct {
			Status struct {
				Conditions []struct {
					Type   string `json:"type"`
					Status string `json:"status"`
				} `json:"conditions"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &nodes); err != nil {
		return false
	}
	for _, n := range nodes.Items {
		for _, c := range n.Status.Conditions {
			if c.Type == "Ready" && c.Status == "True" {
				return true
			}
		}
	}
	return false
}

// BuilderImagePresent mirrors k8s.builder_image_present; nil = cannot
// verify here (no crictl) which doctor renders as SKIP.
func (r *kubectlRunner) BuilderImagePresent(image string) *bool {
	repo, tag := lastPartition(image, ":")
	for _, cmd := range [][]string{{"k3s", "crictl", "images"}, {"crictl", "images"}} {
		out, err := exec.Command(cmd[0], cmd[1:]...).Output()
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(out), "\n")[1:] {
			cols := strings.Fields(line)
			if len(cols) >= 2 && cols[0] == repo && cols[1] == tag {
				t := true
				return &t
			}
		}
		f := false
		return &f
	}
	return nil
}

// ExecInPod mirrors doctor parity_checks kubectl exec.
func (r *kubectlRunner) ExecInPod(namespace, name, script string) (int, string) {
	out, rc := r.Run("-n", namespace, "exec", "deploy/"+name, "--", "sh", "-ec", script)
	return rc, out
}

func lastPartition(s, sep string) (string, string) {
	if i := strings.LastIndex(s, sep); i >= 0 {
		return s[:i], s[i+len(sep):]
	}
	return s, ""
}
