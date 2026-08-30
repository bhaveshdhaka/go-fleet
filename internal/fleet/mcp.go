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

// cmdMcp serves the MCP protocol over stdio. It takes no flags in phase 1.
func cmdMcp(args []string) int {
	if len(args) > 0 {
		fmt.Fprintln(os.Stderr, "usage: fleet mcp   (stdio MCP server, read-only; WO-22)")
		return 2
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
			"Mutations (promote/approve/ops build|deploy|rollback|remove) are NOT " +
			"exposed; use the fleet CLI under its actor policy instead.",
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	// --- tools (read verbs, --json machine contract) --------------------

	toolStatus := func(ctx context.Context, req *mcp.CallToolRequest, in struct {
		Component string `json:"component,omitempty" jsonschema:"optional component filter"`
	}) (*mcp.CallToolResult, any, error) {
		argv := []string{"status", "--json"}
		if in.Component != "" {
			argv = append(argv, in.Component)
		}
		return mcpRunJSON(ctx, p.Root, argv)
	}
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "fleet_status",
		Description: "Registered components with kind and lifecycle stage (fleet status --json).",
	}, toolStatus)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "fleet_doctor",
		Description: "Registry/state/gate/journal drift check (fleet doctor --json). ok=false lists issues.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, any, error) {
		return mcpRunJSON(ctx, p.Root, []string{"doctor", "--json"})
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "fleet_next",
		Description: "Next legal action per the guidance engine (fleet next --json): same state -> same action.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, any, error) {
		return mcpRunJSON(ctx, p.Root, []string{"next", "--json"})
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "fleet_check",
		Description: "Predicates P1-P6 report for workorder authoring drift (fleet check --json).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, any, error) {
		return mcpRunJSON(ctx, p.Root, []string{"check", "--json"})
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "fleet_sites",
		Description: "Managed sites registry (fleet site list --json).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, any, error) {
		return mcpRunJSON(ctx, p.Root, []string{"site", "list", "--json"})
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "fleet_wo_list",
		Description: "Workorder list with status headers (fleet wo list). Text output.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, any, error) {
		return mcpRunText(ctx, p.Root, []string{"wo", "list"})
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
		return mcpRunText(ctx, p.Root, []string{"wo", "show", in.ID})
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "ops_status",
		Description: "Site observation: cluster reachability + per-service registry/state (fleet ops status --json).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in struct {
		Site string `json:"site,omitempty" jsonschema:"optional site name when multiple sites are registered"`
	}) (*mcp.CallToolResult, any, error) {
		return mcpRunJSON(ctx, p.Root, mcpOpsArgs("status", in.Site))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "ops_doctor",
		Description: "Site doctor: tunnel/ingress/cname/secrets/deployed-image checks (fleet ops doctor --json).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in struct {
		Site string `json:"site,omitempty" jsonschema:"optional site name when multiple sites are registered"`
	}) (*mcp.CallToolResult, any, error) {
		return mcpRunJSON(ctx, p.Root, mcpOpsArgs("doctor", in.Site))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "ops_dns",
		Description: "DNS drift report between registry hosts and Cloudflare CNAMEs (fleet ops dns --json, report ONLY — the mutating --apply form is not exposed here).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in struct {
		Site string `json:"site,omitempty" jsonschema:"optional site name when multiple sites are registered"`
	}) (*mcp.CallToolResult, any, error) {
		return mcpRunJSON(ctx, p.Root, mcpOpsArgs("dns", in.Site))
	})

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

// mcpRun self-execs the fleet binary for one read verb and returns
// (stdout, stderr, exit code). Exit codes are the CLI machine contract:
// 0 ok, 1 fail, 2 usage/policy refusal.
func mcpRun(ctx context.Context, root string, argv []string) (string, string, int) {
	ctx, cancel := context.WithTimeout(ctx, mcpToolTimeout)
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
func mcpRunJSON(ctx context.Context, root string, argv []string) (*mcp.CallToolResult, any, error) {
	stdout, stderr, rc := mcpRun(ctx, root, argv)
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
func mcpRunText(ctx context.Context, root string, argv []string) (*mcp.CallToolResult, any, error) {
	stdout, stderr, rc := mcpRun(ctx, root, argv)
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
