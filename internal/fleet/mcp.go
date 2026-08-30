package fleet

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fleet mcp (WO-22 phase 1) — a READ-ONLY MCP server over the fleet CLI.
//
// House decisions it must never violate (workorders/WO-22.md):
//   - Adapter, not second brain: every tool wraps an EXISTING read verb,
//     executed as a self-execed binary with --json. Zero re-implemented
//     logic; the binary stays the single source of truth (and the only
//     mutation authority — which this surface never reaches).
//   - No ambient resolution (rule 7 / C12d): the child env is an explicit
//     allowlist with FLEET_ROOT pinned; cwd is the resolved root. A client
//     launching this server from any cwd cannot leak ambient credentials
//     or a different repo into a tool call.
//   - Read-only surface: NO mutating verb is exposed. ops dns is bound in
//     its report form only; the mutating --apply form is unreachable.
//   - Secrets: contract files exposed as resources contain key NAMES at
//     most, never values (values live only in the secrets home, WO-14).

// Version is stamped by cmd/fleet main (repo VERSION).
var Version = "dev"

// mcpToolTimeout bounds one self-exec tool call (ops verbs shell out to
// kubectl; a hung cluster must not hang the server session).
const mcpToolTimeout = 120 * time.Second

// cmdMcp serves the MCP protocol over stdio. Default surface is
// READ-ONLY (phase 1, unchanged contract). Mutation tools (phase 2)
// register ONLY behind an explicit opt-in: `fleet mcp --mutations` or
// FLEET_MUTATIONS=1 in the client's server environment. Gating at
// REGISTRATION means a read-only client cannot even see a mutating tool;
// the CLI-side gates (actor policy, approvals, promote re-runs of the
// journey corpus) remain the hard blocks behind every mutation.
func cmdMcp(args []string) int {
	mutations := false
	for _, a := range args {
		switch a {
		case "--mutations":
			mutations = true
		default:
			fmt.Fprintln(os.Stderr, "usage: fleet mcp [--mutations]   (stdio MCP server; read-only unless --mutations, WO-22)")
			return 2
		}
	}
	if os.Getenv("FLEET_MUTATIONS") == "1" {
		mutations = true
	}
	p, rc := mustPaths()
	if rc != 0 {
		return rc
	}

	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "fleet",
		Title:   "fleet control plane",
		Version: Version,
	}, &mcp.ServerOptions{
		Instructions: "Read-only observation of the fleet control plane. " +
			"Mutations (promote/approve/ops build|deploy|rollback) are NOT " +
			"exposed; use the fleet CLI under its actor policy instead.",
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if mutations {
		srv = mcp.NewServer(&mcp.Implementation{
			Name:    "fleet",
			Title:   "fleet control plane",
			Version: Version,
		}, &mcp.ServerOptions{
			Instructions: "fleet control plane. Mutation tools ARE enabled " +
				"(explicit opt-in): every one executes the fleet CLI, so the " +
				"actor policy, approval gates, and the promote-time re-run of " +
				"the journey corpus apply exactly as on the command line. " +
				"Refusals arrive as isError results with the exact fix.",
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		})
	}

	// --- tools (read verbs, --json machine contract) --------------------

	toolStatus := func(ctx context.Context, req *mcp.CallToolRequest, in struct {
		Component string `json:"component,omitempty" jsonschema:"optional component filter"`
	}) (*mcp.CallToolResult, any, error) {
		argv := []string{"status", "--json"}
		if in.Component != "" {
			argv = append(argv, in.Component)
		}
		return mcpRunJSON(ctx, p.Root, mcpToolTimeout, argv)
	}
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "fleet_status",
		Description: "Registered components with kind and lifecycle stage (fleet status --json).",
	}, toolStatus)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "fleet_doctor",
		Description: "Registry/state/gate/journal drift check (fleet doctor --json). ok=false lists issues.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, any, error) {
		return mcpRunJSON(ctx, p.Root, mcpToolTimeout, []string{"doctor", "--json"})
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "fleet_next",
		Description: "Next legal action per the guidance engine (fleet next --json): same state -> same action.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, any, error) {
		return mcpRunJSON(ctx, p.Root, mcpToolTimeout, []string{"next", "--json"})
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "fleet_check",
		Description: "Predicates P1-P6 report for workorder authoring drift (fleet check --json).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, any, error) {
		return mcpRunJSON(ctx, p.Root, mcpToolTimeout, []string{"check", "--json"})
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "fleet_sites",
		Description: "Managed sites registry (fleet site list --json).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, any, error) {
		return mcpRunJSON(ctx, p.Root, mcpToolTimeout, []string{"site", "list", "--json"})
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "fleet_wo_list",
		Description: "Workorder list with status headers (fleet wo list). Text output.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, any, error) {
		return mcpRunText(ctx, p.Root, mcpToolTimeout, []string{"wo", "list"})
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "fleet_wo_show",
		Description: "One workorder, full text (fleet wo show <WO-id>).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in struct {
		ID string `json:"id" jsonschema:"workorder id, e.g. WO-22"`
	}) (*mcp.CallToolResult, any, error) {
		if !validWorkorderID(in.ID) {
			return mcpErrorResult(fmt.Sprintf("invalid workorder id %q", in.ID)), nil, nil
		}
		return mcpRunText(ctx, p.Root, mcpToolTimeout, []string{"wo", "show", in.ID})
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "ops_status",
		Description: "Site observation: cluster reachability + per-service registry/state (fleet ops status --json).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in struct {
		Site string `json:"site,omitempty" jsonschema:"optional site name when multiple sites are registered"`
	}) (*mcp.CallToolResult, any, error) {
		return mcpRunJSON(ctx, p.Root, mcpToolTimeout, mcpOpsArgs("status", in.Site))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "ops_doctor",
		Description: "Site doctor: tunnel/ingress/cname/secrets/deployed-image checks (fleet ops doctor --json).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in struct {
		Site string `json:"site,omitempty" jsonschema:"optional site name when multiple sites are registered"`
	}) (*mcp.CallToolResult, any, error) {
		return mcpRunJSON(ctx, p.Root, mcpToolTimeout, mcpOpsArgs("doctor", in.Site))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "ops_dns",
		Description: "DNS drift report between registry hosts and Cloudflare CNAMEs (fleet ops dns --json, report ONLY — the mutating --apply form is not exposed here).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in struct {
		Site string `json:"site,omitempty" jsonschema:"optional site name when multiple sites are registered"`
	}) (*mcp.CallToolResult, any, error) {
		return mcpRunJSON(ctx, p.Root, mcpToolTimeout, mcpOpsArgs("dns", in.Site))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "ops_verify",
		Description: "Curl a service's public URL and report the HTTP code vs expectation (fleet ops verify <service> [--expect N]). Read-only.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in struct {
		Service string `json:"service" jsonschema:"site service name"`
		Site    string `json:"site,omitempty" jsonschema:"optional site name when multiple sites are registered"`
		Expect  int    `json:"expect,omitempty" jsonschema:"expected HTTP code class start (default 200)"`
	}) (*mcp.CallToolResult, any, error) {
		if in.Service == "" {
			return mcpErrorResult("ops_verify requires a service name"), nil, nil
		}
		argv := []string{"ops", "verify"}
		if in.Site != "" {
			argv = append(argv, "--site", in.Site)
		}
		argv = append(argv, in.Service)
		if in.Expect != 0 {
			argv = append(argv, "--expect", fmt.Sprintf("%d", in.Expect))
		}
		return mcpRunText(ctx, p.Root, mcpToolTimeout, argv)
	})

	if mutations {
		mcpRegisterMutations(srv, p.Root)
	}

	// --- resources (contract files; key names at most, never values) ----

	type fleetResource struct {
		uri, name, desc, mime, rel string
	}
	resources := []fleetResource{
		{"fleet://lifecycle/journal", "journal", "Append-only journal: verify lines, approvals, findings (# lines).", "text/plain", filepath.Join("lifecycle", "journal", "events.log")},
		{"fleet://registry/projects", "projects-registry", "Component registry (ops/PROJECTS.yaml).", "text/yaml", filepath.Join("ops", "PROJECTS.yaml")},
		{"fleet://registry/state", "deployment-state", "Runtime state written only by fleet (ops/state/deployments.yaml).", "text/yaml", filepath.Join("ops", "state", "deployments.yaml")},
		{"fleet://registry/sites", "sites-registry", "Managed sites registry (ops/SITES.yaml).", "text/yaml", filepath.Join("ops", "SITES.yaml")},
		{"fleet://lifecycle/gates", "gates", "Which test units + approvals each stage hop needs (lifecycle/gates.yaml).", "text/yaml", filepath.Join("lifecycle", "gates.yaml")},
	}
	for _, r := range resources {
		r := r
		srv.AddResource(&mcp.Resource{
			URI:         r.uri,
			Name:        r.name,
			Description: r.desc,
			MIMEType:    r.mime,
		}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			// absolute path: the SERVER process runs at the client's cwd;
			// only the self-execed tool children are cwd-pinned.
			b, err := os.ReadFile(filepath.Join(p.Root, r.rel))
			if err != nil {
				return nil, mcp.ResourceNotFoundError(req.Params.URI)
			}
			return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
				URI:      r.uri,
				MIMEType: r.mime,
				Text:     string(b),
			}}}, nil
		})
	}

	if err := srv.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		return failf("mcp: %v", err)
	}
	return 0
}

// mcpOpsArgs builds `ops <verb> [--site S] --json`; dns is report-only by
// construction (--apply is a mutation and is never appended here).
func mcpOpsArgs(verb, site string) []string {
	argv := []string{"ops", verb}
	if site != "" {
		argv = append(argv, "--site", site)
	}
	return append(argv, "--json")
}

// mcpChildEnv is the EXPLICIT allowlist handed to the self-execed fleet
// binary (rule-7 discipline: no ambient resolution through the child).
func mcpChildEnv(root string) []string {
	env := []string{"FLEET_ROOT=" + root}
	if h, err := userHome(); err == nil && h != "" {
		env = append(env, "HOME="+h)
	}
	if p := os.Getenv("PATH"); p != "" {
		env = append(env, "PATH="+p)
	}
	if s := os.Getenv("FLEET_SECRETS_HOME"); s != "" {
		env = append(env, "FLEET_SECRETS_HOME="+s)
	}
	if a := os.Getenv("FLEET_ACTOR"); a != "" {
		env = append(env, "FLEET_ACTOR="+a)
	}
	return env
}

func userHome() (string, error) {
	if h, err := os.UserHomeDir(); err == nil {
		return h, nil
	}
	if u, err := user.Current(); err == nil {
		return u.HomeDir, nil
	}
	return "", fmt.Errorf("no home dir")
}

// mcpRun self-execs the fleet binary for one verb and returns
// (stdout, stderr, exit code). Exit codes are the CLI machine contract:
// 0 ok, 1 fail, 2 usage/policy refusal. Long verbs (build/deploy/
// rollback/promote's gate re-runs) pass their own generous timeout.
func mcpRun(ctx context.Context, root string, timeout time.Duration, argv []string) (string, string, int) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Sprintf("fleet mcp: cannot locate own binary: %v", err), 1
	}
	cmd := exec.CommandContext(ctx, exe, argv...)
	cmd.Dir = root
	cmd.Env = mcpChildEnv(root)
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	rc := 0
	if err != nil {
		rc = 1
		if ee, ok := err.(*exec.ExitError); ok {
			rc = ee.ExitCode()
			if rc < 0 { // killed (timeout) — normalize to failure
				rc = 1
				if errBuf.Len() == 0 {
					errBuf.WriteString("fleet mcp: tool call timed out")
				}
			}
		}
	}
	return outBuf.String(), errBuf.String(), rc
}

// mcpRunJSON wraps a --json verb. Failure states are DATA when the CLI
// still emits its JSON contract (doctor ok=false, ops status cluster=false):
// those come back as normal results. Only non-JSON failures (FLEET ERROR,
// rc=2 refusals) become isError results carrying stderr.
func mcpRunJSON(ctx context.Context, root string, timeout time.Duration, argv []string) (*mcp.CallToolResult, any, error) {
	stdout, stderr, rc := mcpRun(ctx, root, timeout, argv)
	trimmed := strings.TrimSpace(stdout)
	if rc == 0 || (rc == 1 && json.Valid([]byte(trimmed)) && trimmed != "") {
		return &mcp.CallToolResult{
			Content:           []mcp.Content{&mcp.TextContent{Text: trimmed}},
			StructuredContent: json.RawMessage(trimmed),
		}, nil, nil
	}
	return mcpErrorResult(strings.TrimSpace(mcpErrText(stdout, stderr))), nil, nil
}

// mcpRunText wraps a text-verb (wo list/show): stdout verbatim.
func mcpRunText(ctx context.Context, root string, timeout time.Duration, argv []string) (*mcp.CallToolResult, any, error) {
	stdout, stderr, rc := mcpRun(ctx, root, timeout, argv)
	if rc != 0 {
		return mcpErrorResult(strings.TrimSpace(mcpErrText(stdout, stderr))), nil, nil
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: strings.TrimRight(stdout, "\n")}},
	}, nil, nil
}

func mcpErrText(stdout, stderr string) string {
	if strings.TrimSpace(stderr) != "" {
		return stderr
	}
	if strings.TrimSpace(stdout) != "" {
		return stdout
	}
	return "fleet tool call failed"
}

func mcpErrorResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}

// mcpToolTimeouts: long verbs get generous bounds. build = kaniko
// (base ~8min measured); deploy/rollback = rollout waits; promote
// re-runs its gate units (the journey corpus) before hopping.
const (
	mcpTimeoutBuild   = 15 * time.Minute
	mcpTimeoutDeploy  = 10 * time.Minute
	mcpTimeoutPromote = 15 * time.Minute
)

// mcpRegisterMutations installs the phase-2 mutation tools. Called ONLY
// behind the explicit --mutations / FLEET_MUTATIONS=1 opt-in: a default
// server never lists them (asserted by C22a and C22e). Every tool is a
// thin self-exec wrapper — the CLI's gates (approvals, actor policy,
// promote-time journey re-runs, critical-service refusals) are the real
// enforcement and stay in the binary.
func mcpRegisterMutations(srv *mcp.Server, root string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "fleet_approve",
		Description: "Write a stage approval file + journal line (fleet approve <component> <dev|prod> [who]). The actor policy applies: prod approvals are refused for actors outside allowed_actors (.fleet.yaml).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in struct {
		Component string `json:"component" jsonschema:"component name"`
		Stage     string `json:"stage" jsonschema:"dev or prod"`
		Who       string `json:"who,omitempty" jsonschema:"approving actor (defaults to FLEET_ACTOR or agent)"`
	}) (*mcp.CallToolResult, any, error) {
		if in.Component == "" || (in.Stage != "dev" && in.Stage != "prod") {
			return mcpErrorResult("fleet_approve requires component and stage (dev|prod)"), nil, nil
		}
		argv := []string{"approve", in.Component, in.Stage}
		if in.Who != "" {
			argv = append(argv, in.Who)
		}
		return mcpRunText(ctx, root, mcpTimeoutPromote, argv)
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "fleet_promote",
		Description: "Gated stage transition (fleet promote <component> <stage>). Re-runs the gate units — including the journey corpus — right now; refuses without the required approvals. Use dry_run=true to preview.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in struct {
		Component string `json:"component" jsonschema:"component name"`
		Stage     string `json:"stage" jsonschema:"target stage: dev|stage|prod"`
		DryRun    bool   `json:"dry_run,omitempty" jsonschema:"preview the hop without executing"`
	}) (*mcp.CallToolResult, any, error) {
		if in.Component == "" || in.Stage == "" {
			return mcpErrorResult("fleet_promote requires component and stage"), nil, nil
		}
		argv := []string{"promote", in.Component, in.Stage}
		if in.DryRun {
			argv = append(argv, "--dry-run")
		}
		return mcpRunText(ctx, root, mcpTimeoutPromote, argv)
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "ops_build",
		Description: "Kaniko build of a site service (fleet ops build <service>). LONG: base images can take ~8 minutes. Journals BUILT + state.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in struct {
		Service string `json:"service" jsonschema:"site service name"`
		Site    string `json:"site,omitempty" jsonschema:"optional site name"`
	}) (*mcp.CallToolResult, any, error) {
		if in.Service == "" {
			return mcpErrorResult("ops_build requires a service name"), nil, nil
		}
		argv := []string{"ops", "build"}
		if in.Site != "" {
			argv = append(argv, "--site", in.Site)
		}
		argv = append(argv, in.Service)
		return mcpRunText(ctx, root, mcpTimeoutBuild, argv)
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "ops_deploy",
		Description: "Deploy a site service: secrets+manifests+rollout+dns+tunnel+monitor+state (fleet ops deploy <service>). Journals DEPLOYED.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in struct {
		Service string `json:"service" jsonschema:"site service name"`
		Site    string `json:"site,omitempty" jsonschema:"optional site name"`
	}) (*mcp.CallToolResult, any, error) {
		if in.Service == "" {
			return mcpErrorResult("ops_deploy requires a service name"), nil, nil
		}
		argv := []string{"ops", "deploy"}
		if in.Site != "" {
			argv = append(argv, "--site", in.Site)
		}
		argv = append(argv, in.Service)
		return mcpRunText(ctx, root, mcpTimeoutDeploy, argv)
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "ops_rollback",
		Description: "Roll a site service back to the previous tag (fleet ops rollback <service>) + state record.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in struct {
		Service string `json:"service" jsonschema:"site service name"`
		Site    string `json:"site,omitempty" jsonschema:"optional site name"`
	}) (*mcp.CallToolResult, any, error) {
		if in.Service == "" {
			return mcpErrorResult("ops_rollback requires a service name"), nil, nil
		}
		argv := []string{"ops", "rollback"}
		if in.Site != "" {
			argv = append(argv, "--site", in.Site)
		}
		argv = append(argv, in.Service)
		return mcpRunText(ctx, root, mcpTimeoutDeploy, argv)
	})
}
