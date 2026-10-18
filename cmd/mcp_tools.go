package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/urfave/cli/v3"
)

// JSON Schema types a tool parameter can take. An array is always of strings.
const (
	mcpString  = "string"
	mcpInteger = "integer"
	mcpBoolean = "boolean"
	mcpArray   = "array"
)

// mcpParamTenant is accepted by every typed tool, and becomes --org.
const mcpParamTenant = "tenant"

// Positional parameter names.
const (
	paramImage  = "image"
	paramTicket = "ticket"
	paramBuild  = "build"
	paramSet    = "set"
)

// mcpParam is one argument of a typed tool, named after its flag unless name
// says otherwise. With flag empty it is positional,
// in declaration order; otherwise it is passed as --flag=value, so a value
// that starts with a dash can never be read as a flag of its own.
type mcpParam struct {
	name     string
	kind     string
	usage    string
	flag     string
	required bool
	def      any
}

// key is the argument's name in the schema: its own name, or its flag's.
func (p mcpParam) key() string {
	if p.name != "" {
		return p.name
	}
	return p.flag
}

// mcpToolSpec is a typed MCP tool over one darkubectl command. The generic
// `darkubectl` tool already reaches every command; these exist because a model
// picks a named tool with a schema far more reliably than it composes argv.
type mcpToolSpec struct {
	name        string
	description string
	// command is the subcommand path and any fixed flags, space-separated.
	command string
	params  []mcpParam
	// write tools are registered only with --allow-writes, and get --yes
	// where the command prompts, since nobody is there to answer.
	write bool
}

func pApp() mcpParam {
	return mcpParam{name: cmdApp, kind: mcpString, usage: "app name or id", required: true}
}

func pNamespace() mcpParam {
	return mcpParam{kind: mcpString, usage: "only this namespace (project), by name or id", flag: flagNamespace}
}

func pDryRun() mcpParam {
	return mcpParam{name: "dryRun", kind: mcpBoolean, usage: "show the change without writing it", flag: flagDryRun}
}

func pWait() mcpParam {
	return mcpParam{kind: mcpBoolean, usage: "block until the new pods are ready and the old ones are gone", flag: flagWait}
}

func pLimit(what string) mcpParam {
	return mcpParam{kind: mcpInteger, usage: "how many " + what + " to list", flag: flagLimit}
}

func pRegistry() mcpParam {
	return mcpParam{kind: mcpString, usage: "registry name (default hamdocker)", flag: flagRegistry}
}

//nolint:funlen // a table, one entry per tool
func mcpToolSpecs() []mcpToolSpec {
	return []mcpToolSpec{
		// Account and tenant.
		{name: cmdWhoami, command: cmdWhoami,
			description: "Show the signed-in Hamravesh account and every tenant (organization) it can reach. Call this first."},
		{name: "list_namespaces", command: "get namespaces",
			description: "List the tenant's namespaces (projects) and the cluster each runs on."},
		{name: "list_members", command: "get members",
			description: "List the tenant's members and their roles."},
		{name: "list_plans", command: "get plans",
			description: "List the app plans (sizes) an app can be created with or moved to.",
			params:      []mcpParam{{kind: mcpBoolean, usage: "every plan, not just create-eligible app plans", flag: flagAll}}},

		// Apps.
		{name: "list_apps", command: "get apps -o wide",
			description: "List the tenant's apps with namespace, state, replicas, type (creation_method), cluster, image and age.",
			params: []mcpParam{
				pNamespace(),
				{kind: mcpString, usage: "only this creation_method, e.g. redisnew, postgresql, docker_image", flag: flagType},
				{kind: mcpBoolean, usage: "only managed (oneclick) services such as Redis or Postgres", flag: flagManaged},
			}},
		{name: "describe_app", command: pathDescribeApp, params: []mcpParam{pApp()},
			description: "Show the full detail of one app: image, plan and resources, envs (secret values hidden), service, domains, probes, disk."},
		{name: "get_app_spec", command: "get apps -o spec", params: []mcpParam{pApp()},
			description: "Print an app as the YAML spec that `apply` and `create -f` take."},
		{name: "get_pods", command: "get pods", params: []mcpParam{pApp()},
			description: "List an app's pods with readiness, status, restart count and age. The only place a crash loop shows."},
		{name: "app_logs", command: pathLogsApp,
			description: "Print the tail of an app container's logs. previous=true reads the instance that crashed.",
			params: []mcpParam{
				pApp(),
				{kind: mcpString, usage: "pod name (default: the app's first pod)", flag: flagPod},
				{kind: mcpString, usage: "container name (default main)", flag: flagContainer},
				{kind: mcpInteger, usage: "lines from the end of the log", flag: flagTail, def: mcpDefaultTail},
				{kind: mcpBoolean, usage: "logs of the previous, crashed container instance", flag: flagPrevious},
				{kind: mcpBoolean, usage: "prefix every line with its timestamp", flag: flagTimestamps},
			}},
		{name: "get_app_env", command: "get env", params: []mcpParam{pApp()},
			description: "List an app's environment variables. Secret variables are listed by name, without values."},
		{name: "get_domains", command: "get domains",
			description: "Show an app's domains and ingress: DNS status, platform certificate expiry, the CNAME target, SSL settings.",
			params: []mcpParam{
				pApp(),
				{kind: mcpBoolean, usage: "also connect to each host and report the certificate it really serves", flag: flagTLS},
			}},
		{name: "get_suggestions", command: "get suggestions", params: []mcpParam{pApp()},
			description: "Show the platform's configuration advice for an app (missing probes, sizing, and so on)."},
		{name: "get_manifests", command: "get manifests",
			description: "Print the Kubernetes objects the platform renders for an app, as YAML.",
			params:      []mcpParam{pApp(), {kind: mcpString, usage: "only this kind: Deployment, Service, Ingress, ...", flag: flagKind}}},
		{name: "app_history", command: "rollout history",
			description: "Show who changed an app, when, and which fields (the audit log).",
			params: []mcpParam{
				pApp(),
				{kind: mcpString, usage: "how far back, as a duration such as 36h or 30d (default 7d)", flag: flagSince},
				{kind: mcpBoolean, usage: "print the old and new value of every changed field", flag: flagDetails},
			}},
		{name: "rollout_status", command: "rollout status",
			description: "Block until every pod of an app is ready and nothing is terminating; fails on a stalled rollout.",
			params:      []mcpParam{pApp(), {kind: mcpString, usage: "give up after this long, e.g. 90s (default 5m)", flag: flagTimeout}}},

		// Usage and monitoring.
		{name: "app_usage", command: "top app", params: []mcpParam{pApp()},
			description: "Show CPU, memory, throttling and network for each pod of an app, from the platform's metrics."},
		{name: "tenant_usage", command: "top apps",
			description: "Show CPU and memory of every app in the tenant, busiest first.",
			params:      []mcpParam{pNamespace(), {name: "sortBy", kind: mcpString, usage: "memory, cpu or name", flag: flagSortBy}}},
		{name: "list_alerts", command: "get alerts",
			description: "List the tenant's monitoring alerts.",
			params: []mcpParam{
				{kind: mcpBoolean, usage: "only alerts that have not resolved", flag: flagFiring},
				{kind: mcpString, usage: "only alerts raised against this app", flag: cmdApp},
			}},
		{name: "list_notifications", command: "get notifications",
			description: "List the account's notifications (all tenants; Persian text).",
			params:      []mcpParam{pLimit("notifications"), {kind: mcpBoolean, usage: "only unread notifications", flag: flagUnread}}},

		// Builds and the registry.
		{name: "list_builds", command: "get builds", params: []mcpParam{pApp(), pLimit("builds")},
			description: "List a git-backed app's recent image builds, newest first."},
		{name: "build_logs", command: pathBuildLogs,
			description: "Print a build's log, the latest build by default.",
			params:      []mcpParam{pApp(), {name: paramBuild, kind: mcpString, usage: "build id (default: the latest)"}}},
		{name: "list_registries", command: "registry list",
			description: "List the container registries the tenant can push to, with their storage use."},
		{name: "registry_images", command: "registry images", params: []mcpParam{pRegistry()},
			description: "List the image repositories in a registry."},
		{name: "registry_tags", command: "registry tags",
			description: "List an image's manifests and the tags on each, newest first.",
			params:      []mcpParam{{name: paramImage, kind: mcpString, usage: "image repository name", required: true}, pRegistry()}},

		// Data, storage and other products.
		{name: "list_disks", command: "get disks",
			description: "List the tenant's persistent disks and the app or database each belongs to.",
			params:      []mcpParam{pNamespace(), {kind: mcpBoolean, usage: "only disks no app accounts for", flag: flagOrphaned}}},
		{name: "list_databases", command: "get databases",
			description: "List the tenant's managed databases (DBaaS) with role, status and node sizes."},
		{name: "list_services", command: "get services",
			description: "List the tenant's marketplace services (Jira, Rocket.Chat, n8n, ...)."},
		{name: "list_storage", command: "get storage",
			description: "List the tenant's object storages with their S3 endpoint, buckets and access keys."},
		{name: "list_certificates", command: "get certificates",
			description: "List the TLS certificates uploaded to the tenant."},

		// Billing.
		{name: "get_balance", command: "get balance",
			description: "Show the tenant's balance: its cash and gift wallets."},
		{name: "list_invoices", command: "get invoices", params: []mcpParam{pLimit("invoices")},
			description: "List the tenant's monthly invoices, newest first."},

		// Support.
		{name: "list_tickets", command: "ticket list",
			description: "List Hamravesh support tickets.",
			params: []mcpParam{
				{kind: mcpString, usage: "open, closed or all", flag: flagStatus},
				{name: "orgWide", kind: mcpBoolean, usage: "every member's tickets, not just this account's", flag: flagOrgWide},
			}},
		{name: "view_ticket", command: "ticket view",
			description: "Show a support ticket and its conversation.",
			params:      []mcpParam{{name: paramTicket, kind: mcpString, usage: "ticket id", required: true}}},

		// Writes.
		{name: "scale_app", command: "scale app", write: true,
			description: "Set an app's replica count. Every change rolls the app's pods.",
			params: []mcpParam{
				pApp(),
				{kind: mcpInteger, usage: "desired replicas", flag: flagReplicas, required: true},
				pDryRun(),
			}},
		{name: "restart_app", command: "rollout restart", write: true, params: []mcpParam{pApp(), pWait()},
			description: "Replace every pod of an app with a fresh one, like kubectl rollout restart."},
		{name: "set_image", command: "set image", write: true,
			description: "Change the container image an app runs. A tag that cannot be pulled leaves the old pod serving.",
			params: []mcpParam{
				pApp(),
				{name: paramImage, kind: mcpString, usage: "IMAGE[:TAG]", required: true},
				pWait(),
				pDryRun(),
			}},
		{name: "set_env", command: "set env", write: true,
			description: "Set or remove an app's environment variables.",
			params: []mcpParam{
				pApp(),
				{name: paramSet, kind: mcpArray, usage: "variables to set, each NAME=VALUE"},
				{kind: mcpArray, usage: "variable names to remove", flag: flagRemove},
				{kind: mcpBoolean, usage: "the variables are secret (stored in the vault)", flag: flagSecret},
				pDryRun(),
			}},
		{name: "set_domains", command: "set domain", write: true,
			description: "Add or remove the custom domains routed to an app. Point DNS at the CNAME target from get_domains.",
			params: []mcpParam{
				pApp(),
				{kind: mcpArray, usage: "domains to route to the app", flag: flagAdd},
				{kind: mcpArray, usage: "domains to stop routing", flag: flagRemove},
				{name: "redirectHttps", kind: mcpBoolean, usage: "redirect plain HTTP to HTTPS", flag: flagRedirectHTTPS},
				pDryRun(),
			}},
		{name: "autoscale_app", command: "autoscale app", write: true,
			description: "Scale an app on CPU with a HorizontalPodAutoscaler, or turn that off.",
			params: []mcpParam{
				pApp(),
				{kind: mcpInteger, usage: "fewest replicas", flag: flagMin},
				{kind: mcpInteger, usage: "most replicas", flag: flagMax},
				{name: "cpuPercent", kind: mcpInteger, usage: "target average CPU, percent of the request (default 80)", flag: flagCPUPercent},
				{kind: mcpBoolean, usage: "turn autoscaling off", flag: flagDisable},
				pDryRun(),
			}},
		{name: "set_resources", command: "set resources", write: true,
			description: "Change an app's plan (see list_plans), or its memory and CPU on a dynamic plan.",
			params: []mcpParam{
				pApp(),
				{kind: mcpString, usage: "plan name, code name or id", flag: flagPlan},
				{kind: mcpString, usage: "memory limit, e.g. 1500M (dynamic plans only)", flag: flagMemory},
				{kind: mcpString, usage: "CPU request, e.g. 750m (dynamic plans only)", flag: flagCPU},
				pDryRun(),
			}},
		{name: "reply_ticket", command: "ticket reply", write: true,
			description: "Reply to a support ticket. This reaches Hamravesh support staff.",
			params: []mcpParam{
				{name: paramTicket, kind: mcpString, usage: "ticket id", required: true},
				{kind: mcpString, usage: "the reply, in Markdown", flag: flagMessage, required: true},
			}},
	}
}

// schema is the tool's input schema, with the shared tenant parameter added.
func (t mcpToolSpec) schema() *jsonschema.Schema {
	s := &jsonschema.Schema{
		Type:                 "object",
		Properties:           map[string]*jsonschema.Schema{},
		AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}},
	}
	for _, p := range t.allParams() {
		ps := &jsonschema.Schema{Type: p.kind, Description: p.usage}
		if p.kind == mcpArray {
			ps.Items = &jsonschema.Schema{Type: mcpString}
		}
		if p.def != nil {
			ps.Default = json.RawMessage(fmt.Sprint(p.def))
		}
		s.Properties[p.key()] = ps
		if p.required {
			s.Required = append(s.Required, p.key())
		}
	}
	return s
}

func (t mcpToolSpec) allParams() []mcpParam {
	return append(slices.Clone(t.params),
		mcpParam{name: mcpParamTenant, kind: mcpString, usage: "tenant (organization slug), instead of the configured one"})
}

// argv turns a call's arguments into a command line. The SDK does not
// validate input for a tool added with a hand-written schema, so this does:
// an unknown key, a missing required one or a wrong type is an error the
// model can read and correct.
func (t mcpToolSpec) argv(in map[string]any) ([]string, string, error) {
	known := map[string]mcpParam{}
	for _, p := range t.allParams() {
		known[p.key()] = p
	}
	for _, k := range slices.Sorted(maps.Keys(in)) {
		if _, ok := known[k]; !ok {
			return nil, "", fmt.Errorf("unknown argument %q", k)
		}
	}

	argv := strings.Fields(t.command)
	var flags []string
	for _, p := range t.params {
		v, ok := in[p.key()]
		if !ok || v == nil {
			if p.required {
				return nil, "", fmt.Errorf("%q is required", p.key())
			}
			if p.def == nil {
				continue
			}
			v = p.def
		}
		vals, err := mcpValues(p, v)
		if err != nil {
			return nil, "", err
		}
		for _, val := range vals {
			switch {
			case p.flag == "":
				if strings.HasPrefix(val, "-") {
					return nil, "", fmt.Errorf("%q must not start with a dash: %q", p.key(), val)
				}
				argv = append(argv, val)
			case p.kind == mcpBoolean:
				flags = append(flags, "--"+p.flag)
			default:
				flags = append(flags, "--"+p.flag+"="+val)
			}
		}
	}
	argv = append(argv, flags...)
	if t.write && t.prompts() {
		argv = append(argv, "--"+flagYes)
	}
	tenant, _ := in[mcpParamTenant].(string)
	return argv, tenant, nil
}

// prompts reports whether the tool's command asks for confirmation, which is
// whether it has a --yes to skip it with; `ticket reply` sends without asking.
func (t mcpToolSpec) prompts() bool {
	c := NewApp()
	for name := range strings.FieldsSeq(t.command) {
		if c = c.Command(name); c == nil {
			return false
		}
	}
	return lookupFlag([]*cli.Command{c}, flagYes) != nil
}

// mcpValues renders one argument as the strings it contributes to argv: none
// for false or an empty string, one per element for an array.
func mcpValues(p mcpParam, v any) ([]string, error) {
	bad := fmt.Errorf("%q must be a %s, got %T", p.key(), p.kind, v)
	switch p.kind {
	case mcpBoolean:
		b, ok := v.(bool)
		if !ok {
			return nil, bad
		}
		if b {
			return []string{"true"}, nil
		}
		return nil, nil
	case mcpInteger:
		var n float64
		switch x := v.(type) {
		case float64:
			n = x
		case int:
			n = float64(x)
		default:
			return nil, bad
		}
		if n != math.Trunc(n) {
			return nil, bad
		}
		return []string{strconv.FormatInt(int64(n), 10)}, nil
	case mcpArray:
		items, ok := v.([]any)
		if !ok {
			return nil, bad
		}
		out := make([]string, 0, len(items))
		for _, it := range items {
			s, ok := it.(string)
			if !ok {
				return nil, fmt.Errorf("%q must hold strings, got %T", p.key(), it)
			}
			out = append(out, s)
		}
		return out, nil
	default:
		s, ok := v.(string)
		if !ok {
			return nil, bad
		}
		if s == "" {
			return nil, nil
		}
		return []string{s}, nil
	}
}

func (s *mcpServer) registerTyped(server *mcp.Server) {
	for _, t := range mcpToolSpecs() {
		if t.write && !s.allowWrites {
			continue
		}
		server.AddTool(&mcp.Tool{
			Name:        t.name,
			Description: t.description + " Runs `darkubectl " + t.command + "`.",
			InputSchema: t.schema(),
			Annotations: &mcp.ToolAnnotations{
				ReadOnlyHint:    !t.write,
				DestructiveHint: new(t.write),
				OpenWorldHint:   new(true),
			},
		}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return s.callTyped(ctx, t, req.Params.Arguments), nil
		})
	}
}

// callTyped runs one typed tool call. A bad argument is a tool result with
// IsError, not a protocol error, so the model sees why and can correct it.
func (s *mcpServer) callTyped(ctx context.Context, t mcpToolSpec, raw json.RawMessage) *mcp.CallToolResult {
	in := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &in); err != nil {
			return mcpError(t.name + ": arguments: " + err.Error())
		}
	}
	argv, tenant, err := t.argv(in)
	if err != nil {
		return mcpError(t.name + ": " + err.Error())
	}
	return s.run(ctx, tenant, 0, argv)
}
