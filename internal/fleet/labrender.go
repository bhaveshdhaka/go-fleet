package fleet

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Lab renderers (WO-8): object-graph mirrors of labctl/k8s.py. fleet
// renders the SAME k8s objects labctl renders; manifests are applied as
// canonical JSON (kubectl parses both identically — parity contract is
// the applied object graph, asserted against python labctl by
// TestLabRenderParity and corpus C13a).

func asInt(v any) int {
	if n, ok := v.(int); ok {
		return n
	}
	return 0
}

func orStr(v any, def string) string {
	if s := asString(v); s != "" {
		return s
	}
	return def
}

// labEnvOrder recovers the registry FILE order of a service's env keys.
// python dicts preserve file order and labctl emits container.env in that
// order; the mini parser returns unordered maps, so the order is recovered
// from the raw lines (block style and inline flow style both covered).
func labEnvOrder(labRoot, name string, svc map[string]any) []string {
	env := asMap(svc["env"])
	if env == nil {
		return nil
	}
	lines, err := readLines(filepath.Join(labRoot, "config", "registry.yaml"))
	if err == nil {
		start := -1
		for i, ln := range lines {
			if strings.TrimRight(ln, " ") == "  "+name+":" {
				start = i
				break
			}
		}
		if start >= 0 {
			for i := start + 1; i < len(lines); i++ {
				ln := lines[i]
				t := strings.TrimLeft(ln, " ")
				if t == "" || strings.HasPrefix(t, "#") {
					continue
				}
				indent := len(ln) - len(t)
				if indent <= 2 {
					break // next service or section
				}
				if indent == 4 && strings.HasPrefix(t, "env:") {
					inline := strings.TrimSpace(strings.TrimPrefix(t, "env:"))
					if strings.HasPrefix(inline, "{") && strings.HasSuffix(inline, "}") {
						inner := strings.TrimSpace(inline[1 : len(inline)-1])
						var keys []string
						for _, part := range splitFlow(inner) {
							if k, _, ok := splitYAMLKey(strings.TrimSpace(part)); ok {
								keys = append(keys, k)
							}
						}
						return keys
					}
					if inline == "" {
						var keys []string
						for j := i + 1; j < len(lines); j++ {
							l2 := lines[j]
							t2 := strings.TrimLeft(l2, " ")
							if t2 == "" || strings.HasPrefix(t2, "#") {
								continue
							}
							ind2 := len(l2) - len(t2)
							if ind2 <= 4 {
								break
							}
							if k, _, ok := splitYAMLKey(t2); ok {
								keys = append(keys, k)
							}
						}
						if len(keys) > 0 {
							return keys
						}
					}
					break
				}
			}
		}
	}
	return sortedKeys(env)
}

// renderLabService mirrors k8s.py render_service.
func renderLabService(lv *LabView, root, name string, svc map[string]any, image string) []any {
	labRoot := lv.Site.LabRootAbs(root)
	ns := asString(svc["namespace"])
	port := asInt(svc["port"])
	storage := asMap(svc["storage"])
	mountsSpec := asList(svc["mounts"])
	var pvcMounts, hostMounts []map[string]any
	for _, mAny := range mountsSpec {
		m := asMap(mAny)
		if m == nil {
			continue
		}
		if asString(m["host"]) != "" {
			hostMounts = append(hostMounts, m)
		} else {
			pvcMounts = append(pvcMounts, m)
		}
	}

	volumeMounts := []any{}
	volumes := []any{}
	if len(pvcMounts) > 0 {
		for _, m := range pvcMounts {
			vm := map[string]any{"name": "data", "mountPath": asString(m["path"])}
			if asString(m["sub"]) != "" {
				vm["subPath"] = asString(m["sub"])
			}
			volumeMounts = append(volumeMounts, vm)
		}
		volumes = append(volumes, map[string]any{
			"name":                  "data",
			"persistentVolumeClaim": map[string]any{"claimName": name + "-data"},
		})
	} else if len(storage) > 0 {
		mountPath := orStr(storage["mount"], "/data")
		volumeMounts = append(volumeMounts, map[string]any{"name": "data", "mountPath": mountPath})
		volumes = append(volumes, map[string]any{
			"name":                  "data",
			"persistentVolumeClaim": map[string]any{"claimName": name + "-data"},
		})
	}
	kubeMountIdx := -1
	for i, m := range hostMounts {
		if strings.HasSuffix(asString(m["host"]), "k3s.yaml") {
			kubeMountIdx = i
			break
		}
	}
	for i, m := range hostMounts {
		hp := map[string]any{"path": asString(m["host"]), "type": orStr(m["type"], "Directory")}
		volumes = append(volumes, map[string]any{"name": fmt.Sprintf("host%d", i), "hostPath": hp})
		vm := map[string]any{"name": fmt.Sprintf("host%d", i), "mountPath": asString(m["path"])}
		if i == kubeMountIdx {
			vm["mountPath"] = "/kubeconfig-src/" + filepath.Base(asString(m["path"]))
		}
		if asBool(m["readOnly"]) {
			vm["readOnly"] = true
		}
		volumeMounts = append(volumeMounts, vm)
	}

	initContainers := []any{}
	if kubeMountIdx >= 0 {
		km := hostMounts[kubeMountIdx]
		target := asString(km["path"])
		base := filepath.Base(asString(km["path"]))
		hostVol := fmt.Sprintf("host%d", kubeMountIdx)
		srcPath := "/kubeconfig-src/" + base
		volumes = append(volumes, map[string]any{"name": "kubeconfig", "emptyDir": map[string]any{}})
		volumeMounts = append(volumeMounts, map[string]any{"name": "kubeconfig", "mountPath": filepath.Dir(target)})
		initContainers = append(initContainers, map[string]any{
			"name":  "kubeconfig",
			"image": "busybox:1.36",
			"command": []any{"sh", "-ec",
				fmt.Sprintf(`sed "s#127.0.0.1#${KUBE_API_HOST}#g" "%s" > "/out/%s"`, srcPath, base)},
			"env": []any{map[string]any{
				"name":      "KUBE_API_HOST",
				"valueFrom": map[string]any{"fieldRef": map[string]any{"fieldPath": "status.hostIP"}},
			}},
			"volumeMounts": []any{
				map[string]any{"name": hostVol, "mountPath": srcPath, "readOnly": true},
				map[string]any{"name": "kubeconfig", "mountPath": "/out"},
			},
		})
	}

	env := []any{}
	for _, k := range labEnvOrder(labRoot, name, svc) {
		env = append(env, map[string]any{"name": k, "value": asString(svc["env"].(map[string]any)[k])})
	}
	if kubeMountIdx >= 0 {
		hasKubeconfigEnv := false
		for _, eAny := range env {
			if asString(asMap(eAny)["name"]) == "KUBECONFIG" {
				hasKubeconfigEnv = true
			}
		}
		if !hasKubeconfigEnv {
			env = append(env, map[string]any{"name": "KUBECONFIG", "value": asString(hostMounts[kubeMountIdx]["path"])})
		}
	}

	// python: svc.get("probePath", "/") — a present-but-null key stays null.
	var probePath any = "/"
	if v, ok := svc["probePath"]; ok {
		probePath = v
	}
	resources := asMap(svc["resources"])
	if len(resources) == 0 {
		resources = map[string]any{
			"requests": map[string]any{"memory": "128Mi", "cpu": "50m"},
			"limits":   map[string]any{"memory": "1Gi"},
		}
	}

	container := map[string]any{
		"name":            name,
		"image":           image,
		"imagePullPolicy": "IfNotPresent",
		"ports":           []any{map[string]any{"containerPort": port}},
		"volumeMounts":    volumeMounts,
		"resources":       resources,
		"readinessProbe": map[string]any{
			"httpGet":             map[string]any{"path": probePath, "port": port},
			"initialDelaySeconds": 10,
			"periodSeconds":       10,
		},
		"livenessProbe": map[string]any{
			"httpGet":             map[string]any{"path": probePath, "port": port},
			"initialDelaySeconds": 30,
			"periodSeconds":       30,
		},
	}
	if len(env) > 0 {
		container["env"] = env
	}
	if _, err := os.Stat(filepath.Join(lv.Site.secretsDir(root), name+".env")); err == nil {
		container["envFrom"] = []any{map[string]any{"secretRef": map[string]any{"name": name + "-env"}}}
	}
	if argsSpec := asList(svc["args"]); len(argsSpec) > 0 {
		args := []any{}
		for _, a := range argsSpec {
			args = append(args, asString(a))
		}
		container["args"] = args
	}
	if v, ok := svc["runAsUser"]; ok && v != nil {
		container["securityContext"] = map[string]any{"runAsUser": asInt(v)}
	}

	dep := map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]any{
			"name":      name,
			"namespace": ns,
			"labels":    map[string]any{"app": name},
		},
		"spec": map[string]any{
			"replicas": 1,
			"selector": map[string]any{"matchLabels": map[string]any{"app": name}},
			"template": map[string]any{
				"metadata": map[string]any{"labels": map[string]any{"app": name}},
				"spec":     map[string]any{"containers": []any{container}, "volumes": volumes},
			},
		},
	}
	sa := asString(svc["serviceAccount"])
	if sa != "" {
		spec := asMap(asMap(dep["spec"])["template"])
		tspec := asMap(spec["spec"])
		tspec["serviceAccountName"] = sa
		tspec["automountServiceAccountToken"] = true
		if len(initContainers) > 0 {
			tspec["initContainers"] = initContainers
		}
	} else if len(initContainers) > 0 {
		spec := asMap(asMap(dep["spec"])["template"])
		asMap(spec["spec"])["initContainers"] = initContainers
	}
	svcDoc := map[string]any{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata":   map[string]any{"name": name, "namespace": ns},
		"spec": map[string]any{
			"selector": map[string]any{"app": name},
			"ports":    []any{map[string]any{"port": port, "targetPort": port}},
		},
	}
	docs := []any{dep, svcDoc}
	if sa != "" {
		docs = append(docs, renderLabSAClusterAdmin(sa, ns)...)
	}
	hasPVC := false
	for _, vAny := range volumes {
		if asMap(vAny)["persistentVolumeClaim"] != nil {
			hasPVC = true
		}
	}
	if hasPVC {
		docs = append(docs, renderLabPVC(name, ns, orStr(storage["size"], "1Gi")))
	}
	return docs
}

// renderLabSAClusterAdmin mirrors k8s.py render_sa_cluster_admin (owner
// decision 2026-08-25: single-operator lab, SA carries cluster-admin).
func renderLabSAClusterAdmin(name, namespace string) []any {
	return []any{
		map[string]any{
			"apiVersion": "v1",
			"kind":       "ServiceAccount",
			"metadata":   map[string]any{"name": name, "namespace": namespace},
		},
		map[string]any{
			"apiVersion": "rbac.authorization.k8s.io/v1",
			"kind":       "ClusterRoleBinding",
			"metadata":   map[string]any{"name": name + "-" + namespace + "-admin"},
			"roleRef": map[string]any{
				"apiGroup": "rbac.authorization.k8s.io",
				"kind":     "ClusterRole",
				"name":     "cluster-admin",
			},
			"subjects": []any{map[string]any{
				"kind": "ServiceAccount", "name": name, "namespace": namespace,
			}},
		},
	}
}

// renderLabPVC mirrors k8s.py render_pvc.
func renderLabPVC(name, namespace, size string) map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "PersistentVolumeClaim",
		"metadata":   map[string]any{"name": name + "-data", "namespace": namespace},
		"spec": map[string]any{
			"accessModes": []any{"ReadWriteOnce"},
			"resources":   map[string]any{"requests": map[string]any{"storage": size}},
		},
	}
}

// renderLabKanikoJob mirrors k8s.py render_kaniko_job (one job = one
// kaniko build step; 8Gi limit — vite OOMs at 2Gi, measured 2026-08-23).
func renderLabKanikoJob(ns, jobName, contextPath, destination string, extraArgs []string, dockerfile string) map[string]any {
	args := []any{
		"--context=" + contextPath,
		"--dockerfile=" + dockerfile,
		"--destination=" + destination,
		"--cache=true",
		"--cache-repo=" + strings.SplitN(destination, "/", 2)[0] + "/buildcache",
		"--insecure",
	}
	for _, a := range extraArgs {
		args = append(args, a)
	}
	return map[string]any{
		"apiVersion": "batch/v1",
		"kind":       "Job",
		"metadata": map[string]any{
			"name":      jobName,
			"namespace": ns,
			"labels":    map[string]any{"app": jobName},
		},
		"spec": map[string]any{
			"backoffLimit":          0,
			"activeDeadlineSeconds": 3600,
			"template": map[string]any{
				"metadata": map[string]any{"labels": map[string]any{"app": jobName}},
				"spec": map[string]any{
					"restartPolicy": "Never",
					"containers": []any{map[string]any{
						"name":  "build",
						"image": LabKanikoImage,
						"args":  args,
						"volumeMounts": []any{map[string]any{
							"name": "workspaces", "mountPath": "/workspace",
						}},
						"resources": map[string]any{
							"requests": map[string]any{"memory": "1Gi", "cpu": "500m"},
							"limits":   map[string]any{"memory": "8Gi"},
						},
					}},
					"volumes": []any{map[string]any{
						"name":     "workspaces",
						"hostPath": map[string]any{"path": "/workspace", "type": "Directory"},
					}},
				},
			},
		},
	}
}

// labBuildTag mirrors labctl.cli._tag: UTC "%Y.%m.%d-r%H%M%S".
func labBuildTag(now time.Time) string {
	return now.UTC().Format("2006.01.02") + "-r" + now.UTC().Format("150405")
}

// labImageTag mirrors k8s.py image_tag.
func labImageTag(image string) string {
	last := image
	if i := strings.LastIndex(image, "/"); i >= 0 {
		last = image[i+1:]
	}
	if i := strings.LastIndex(last, ":"); i >= 0 {
		return last[i+1:]
	}
	return "latest"
}

// labOverlayDir mirrors cli._overlay_dir: images/<name>/Dockerfile exists.
func labOverlayDir(labRoot, name string) bool {
	_, err := os.Stat(filepath.Join(labRoot, "images", name, "Dockerfile"))
	return err == nil
}

// labServiceImage mirrors k8s.service_image: declared image, else the
// last build from the in-cluster registry.
func labServiceImage(lv *LabView, name string, svc map[string]any) (string, error) {
	if img := asString(svc["image"]); img != "" {
		return img, nil
	}
	tag := stateEntry(lv.Builds, name, "tag")
	if tag == "" {
		return "", fmt.Errorf("no build recorded for '%s' — run: ./lab build %s", name, name)
	}
	return labRegistryHost(lv.Site.Namespace) + "/" + name + ":" + tag, nil
}

// renderLabTunnelIngress mirrors k8s.tunnel_ingress_from_registry.
func renderLabTunnelIngress(lv *LabView) []any {
	out := []any{}
	for _, rs := range lv.RoutedServices() {
		ns := orStr(rs.Svc["namespace"], "default")
		out = append(out, map[string]any{
			"hostname": asString(rs.Svc["host"]),
			"service":  fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", rs.Name, ns, asInt(rs.Svc["port"])),
		})
	}
	return out
}

// renderLabMonitorDocs mirrors cli.cmd_monitor + k8s.render_monitor_cms +
// k8s.render_dashboard: gatus ConfigMap (config.yaml string emitted by
// pyGatusDump), dashboard-state ConfigMap (python json.dumps strings),
// dashboard nginx/render ConfigMaps (template files with @SLUG@ swapped),
// the dashboard Deployment + Service.
func renderLabMonitorDocs(lv *LabView, root, slug string) ([]any, error) {
	labRoot := lv.Site.LabRootAbs(root)
	endpoints := []any{}
	hosts := map[string]any{}
	for _, name := range lv.LabServiceNames() {
		svc := lv.LabServices()[name]
		host := asString(svc["host"])
		if host == "" {
			continue
		}
		hosts[name] = host
		if asBool(svc["enabled"]) {
			endpoints = append(endpoints, map[string]any{
				"name":       name,
				"url":        "https://" + host,
				"interval":   "60s",
				"conditions": []any{"[STATUS] == 200"},
			})
		}
	}
	projects := asMap(lv.Registry["projects"])
	proj := asMap(projects["go-fleet"])
	if proj == nil {
		proj = asMap(projects["sos-lab"])
	}
	principles := asMap(proj["principles"])
	if principles == nil {
		principles = map[string]any{}
	}
	ns := lv.Site.Namespace

	nginxRaw, err := os.ReadFile(filepath.Join(labRoot, "templates", "dashboard-nginx.conf"))
	if err != nil {
		return nil, fmt.Errorf("missing %s", filepath.Join(labRoot, "templates", "dashboard-nginx.conf"))
	}
	nginxConf := strings.ReplaceAll(string(nginxRaw), "@SLUG@", slug)

	cms := []any{
		map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata":   map[string]any{"name": "gatus-config", "namespace": ns},
			"data":       map[string]any{"config.yaml": pyGatusDump(endpoints)},
		},
		map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata":   map[string]any{"name": "dashboard-state", "namespace": ns},
			"data": map[string]any{
				"state.json":      pyJSONDumps(lv.Deployed),
				"principles.json": pyJSONDumps(principles),
				"hosts.json":      pyJSONDumps(hosts),
			},
		},
		map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata":   map[string]any{"name": "dashboard-nginx", "namespace": ns},
			"data":       map[string]any{"default.conf": nginxConf},
		},
	}
	dep := map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]any{"name": "sos-dashboard", "namespace": ns, "labels": map[string]any{"app": "sos-dashboard"}},
		"spec": map[string]any{
			"replicas": 1,
			"selector": map[string]any{"matchLabels": map[string]any{"app": "sos-dashboard"}},
			"template": map[string]any{
				"metadata": map[string]any{"labels": map[string]any{"app": "sos-dashboard"}},
				"spec": map[string]any{
					"containers": []any{
						map[string]any{
							"name":            "nginx",
							"image":           "nginx:alpine",
							"imagePullPolicy": "IfNotPresent",
							"ports":           []any{map[string]any{"containerPort": 80}},
							"volumeMounts": []any{
								map[string]any{"name": "webroot", "mountPath": "/usr/share/nginx/html"},
								map[string]any{"name": "nginx-conf", "mountPath": "/etc/nginx/conf.d/default.conf", "subPath": "default.conf", "readOnly": true},
							},
							"resources": map[string]any{
								"requests": map[string]any{"memory": "32Mi", "cpu": "25m"},
								"limits":   map[string]any{"memory": "128Mi"},
							},
							"readinessProbe": map[string]any{
								"httpGet":             map[string]any{"path": "/" + slug + "/", "port": 80},
								"initialDelaySeconds": 5,
								"periodSeconds":       10,
							},
						},
						map[string]any{
							"name":            "renderer",
							"image":           rendererImage(lv),
							"imagePullPolicy": "IfNotPresent",
							"volumeMounts": []any{
								map[string]any{"name": "webroot", "mountPath": "/webroot"},
								map[string]any{"name": "state-cm", "mountPath": "/state", "readOnly": true},
							},
							"resources": map[string]any{
								"requests": map[string]any{"memory": "64Mi", "cpu": "25m"},
								"limits":   map[string]any{"memory": "256Mi"},
							},
						},
					},
					"volumes": []any{
						map[string]any{"name": "webroot", "emptyDir": map[string]any{}},
						map[string]any{"name": "nginx-conf", "configMap": map[string]any{"name": "dashboard-nginx"}},
						map[string]any{"name": "state-cm", "configMap": map[string]any{"name": "dashboard-state"}},
					},
				},
			},
		},
	}
	svcDoc := map[string]any{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata":   map[string]any{"name": "sos-dashboard", "namespace": ns},
		"spec": map[string]any{
			"selector": map[string]any{"app": "sos-dashboard"},
			"ports":    []any{map[string]any{"port": 80, "targetPort": 80}},
		},
	}
	return append(cms, dep, svcDoc), nil
}

// rendererImage resolves the fleet-built dashboard-render image (the Go
// port; the python:3.12-alpine era is gone — WO-20 piece 4).
func rendererImage(lv *LabView) string {
	tag := asString(asMap(lv.Builds["dashboard-render"])["tag"])
	if tag == "" {
		return labRegistryHost(lv.Site.Namespace) + "/dashboard-render:latest"
	}
	return labRegistryHost(lv.Site.Namespace) + "/dashboard-render:" + tag
}

// labDashboardSlug mirrors cli._dashboard_slug (DASHBOARD_SLUG from the
// site's secrets/dashboard.env; the value is a URL path token, not a
// credential — printed only in the dashboard URL like ./lab url does).
// Takes the SITE SECRETS DIR (honoring the secrets_dir override).
func labDashboardSlug(secretsDir string) (string, error) {
	path := filepath.Join(secretsDir, "dashboard.env")
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("missing %s — create it with DASHBOARD_SLUG=<hex>", path)
	}
	for _, ln := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(ln, "DASHBOARD_SLUG=") {
			v := strings.TrimSpace(strings.TrimPrefix(ln, "DASHBOARD_SLUG="))
			if v != "" {
				return v, nil
			}
		}
	}
	return "", fmt.Errorf("DASHBOARD_SLUG not set in %s", path)
}
