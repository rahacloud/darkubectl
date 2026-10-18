package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/urfave/cli/v3"
)

// Command names the MCP tools build argv from.
const (
	progName    = "darkubectl"
	cmdGet      = "get"
	cmdApps     = "apps"
	cmdPods     = "pods"
	cmdLogs     = "logs"
	cmdDescribe = "describe"
	cmdWhoami   = "whoami"
)

// Command paths the MCP policy and tools both name.
const (
	pathDescribeApp = cmdDescribe + " " + cmdApp
	pathLogsApp     = cmdLogs + " " + cmdApp
	pathBuildLogs   = "build " + cmdLogs
)

const (
	flagAllowWrites  = "allow-writes"
	flagAllowSecrets = "allow-secrets"
	flagMCPTimeout   = "timeout"

	// mcpMaxOutput caps what one tool call hands back. A model's context is
	// the scarce thing here, and a full `get manifests` of a tenant is not.
	mcpMaxOutput      = 256 << 10
	mcpDefaultTail    = 200
	mcpWaitDelay      = 5 * time.Second
	mcpDefaultTimeout = 2 * time.Minute
)

// mcpAccess is how much a command can do, which decides whether the MCP server
// runs it. The levels are ordered: each one needs every permission below it.
type mcpAccess int

const (
	accessRead mcpAccess = iota
	accessSecret
	accessWrite
	accessBlocked
)

func (a mcpAccess) String() string {
	switch a {
	case accessRead:
		return "read"
	case accessSecret:
		return "secret"
	case accessWrite:
		return "write"
	case accessBlocked:
		return "blocked"
	default:
		return "unknown"
	}
}

// mcpPolicy holds the commands whose access is not "write", keyed by their
// canonical path. Anything absent is a write, so a command added later is
// gated until someone decides otherwise — the failure mode is a refusal, not
// an AI agent quietly deleting an app. Every `get` subcommand is a read unless
// listed here, and a bare command group only prints its help.
var mcpPolicy = map[string]mcpAccess{
	"version":           accessRead,
	cmdWhoami:           accessRead,
	pathDescribeApp:     accessRead,
	pathLogsApp:         accessRead,
	"top app":           accessRead,
	"top apps":          accessRead,
	"wait app":          accessRead,
	"rollout status":    accessRead,
	"rollout history":   accessRead,
	"build list":        accessRead,
	pathBuildLogs:       accessRead,
	"registry list":     accessRead,
	"registry images":   accessRead,
	"registry tags":     accessRead,
	"registry gc":       accessRead,
	"ticket list":       accessRead,
	"ticket view":       accessRead,
	"ticket types":      accessRead,
	"ticket priorities": accessRead,
	"config view":       accessRead,
	"get deploy-token":  accessSecret,
	"get ci-config":     accessSecret,
	"registry password": accessSecret,
	"terminal":          accessBlocked,
	"login":             accessBlocked,
	"completion":        accessBlocked,
	"mcp":               accessBlocked,
	"tunnel connect":    accessBlocked,
	"config set-token":  accessBlocked,
	"config use-tenant": accessBlocked,
	"config add-tenant": accessBlocked,
}

// Flags that never come back on their own (a watch, a follow, a TUI) are
// refused rather than left to run into the timeout, and the reason says what
// to do instead.
var mcpBlockedFlags = map[string]map[string]string{
	pathDescribeApp: {"interactive": "the interactive viewer needs a terminal; drop -i"},
	"get pods":      {"watch": "a watch never returns; poll `get pods` instead"},
	pathLogsApp:     {"follow": "a follow never returns; use --tail and call again"},
	pathBuildLogs:   {"follow": "a follow never returns; read the log again instead"},
}

// Root flags that pick the identity the server acts as. A tool call may
// choose the tenant, not the credential or the API it is sent to.
var mcpIdentityFlags = []string{flagConfig, flagToken, flagBaseURL}

var (
	errMCPEmptyArgs = errors.New("args is empty: pass a command such as [\"get\", \"apps\"]")
	errMCPDashDash  = errors.New("a `--` before the command is not allowed")
)

// mcpInvocation is a resolved command line: the canonical subcommand path, the
// named flags given anywhere in it, and the positional arguments after the path.
type mcpInvocation struct {
	path  []string
	flags map[string]bool
	args  []string
	help  bool
	// group is set when the path ends on a command that has subcommands.
	group bool
}

func (inv mcpInvocation) key() string { return strings.Join(inv.path, " ") }

// resolveMCPInvocation walks args through the command tree the same way
// urfave/cli will, so the policy is decided on the command that would run and
// not on a guess from the first word. Flags are recorded by their canonical
// (first) name so an alias like -f cannot slip past a check written for --follow.
func resolveMCPInvocation(root *cli.Command, args []string) (mcpInvocation, error) {
	inv := mcpInvocation{flags: map[string]bool{}, group: len(root.Commands) > 0}
	cur := root
	scope := []*cli.Command{root}
	descending := true
	for i := 0; i < len(args); i++ {
		tok := args[i]
		if tok == "--" {
			if descending {
				return inv, errMCPDashDash
			}
			inv.args = append(inv.args, args[i+1:]...)
			break
		}
		if strings.HasPrefix(tok, "-") && len(tok) > 1 {
			name, _, hasValue := strings.Cut(strings.TrimLeft(tok, "-"), "=")
			if name == "help" || name == "h" {
				inv.help = true
				continue
			}
			f := lookupFlag(scope, name)
			if f == nil {
				return inv, fmt.Errorf("unknown flag %q for `%s`", tok, strings.TrimSpace(progName+" "+inv.key()))
			}
			inv.flags[f.Names()[0]] = true
			if tv, ok := f.(interface{ TakesValue() bool }); ok && tv.TakesValue() && !hasValue {
				i++
			}
			continue
		}
		if descending {
			if tok == "help" && cur == root {
				inv.help = true
				continue
			}
			if sub := cur.Command(tok); sub != nil {
				cur = sub
				inv.group = len(sub.Commands) > 0
				scope = append(scope, sub)
				inv.path = append(inv.path, sub.Name)
				continue
			}
			descending = false
		}
		inv.args = append(inv.args, tok)
	}
	return inv, nil
}

func lookupFlag(scope []*cli.Command, name string) cli.Flag {
	for _, c := range slices.Backward(scope) {
		for _, f := range c.Flags {
			if slices.Contains(f.Names(), name) {
				return f
			}
		}
	}
	return nil
}

// classify decides what an invocation can do. --help is always a read; the
// rest comes from the policy table, raised by the few flags and arguments
// that change what a command touches.
func (inv mcpInvocation) classify() (mcpAccess, string) {
	if inv.help || (inv.group && len(inv.args) == 0) {
		return accessRead, ""
	}
	key := inv.key()
	for flag, why := range mcpBlockedFlags[key] {
		if inv.flags[flag] {
			return accessBlocked, why
		}
	}
	for _, f := range mcpIdentityFlags {
		if inv.flags[f] {
			return accessBlocked, "--" + f + " is fixed by the MCP server's own command line"
		}
	}
	switch key {
	case "api":
		// A GET can still read a secret (read_all_vault_secret_envs is one),
		// so the raw escape hatch is never a plain read.
		method := strings.ToUpper(firstOr(inv.args, ""))
		if method == http.MethodGet || method == http.MethodOptions {
			return accessSecret, ""
		}
		return accessWrite, ""
	case "get env":
		if inv.flags["show-secrets"] {
			return accessSecret, ""
		}
	case "get orphans":
		// --kubectl names a binary to run on this machine.
		if inv.flags["kubectl"] {
			return accessWrite, ""
		}
	}
	if a, ok := mcpPolicy[key]; ok {
		return a, ""
	}
	if len(inv.path) > 0 && inv.path[0] == cmdGet {
		return accessRead, ""
	}
	return accessWrite, ""
}

func firstOr(s []string, def string) string {
	if len(s) == 0 {
		return def
	}
	return s[0]
}

// mcpServer runs darkubectl commands on behalf of an MCP client. Each call is
// a child process of this same binary rather than an in-process Run, because
// every command prints to os.Stdout — which, here, is the JSON-RPC channel.
type mcpServer struct {
	self         string
	baseArgs     []string
	allowWrites  bool
	allowSecrets bool
	timeout      time.Duration
}

func newMCPCommand() *cli.Command {
	return &cli.Command{
		Name:  "mcp",
		Usage: "Serve darkubectl to AI tools over the Model Context Protocol (stdio)",
		Description: "Speaks MCP on stdin/stdout, so an AI client can list, inspect and (when allowed)\n" +
			"change Darkube apps through the same commands a person runs. Register it with a\n" +
			"client, for example Claude Code:\n\n" +
			"  claude mcp add darkube -- darkubectl mcp\n" +
			"  claude mcp add darkube-acme -- darkubectl --org acme mcp --allow-writes\n\n" +
			"It uses this machine's config and credential, so run `darkubectl login` first.\n" +
			"--org, --config, --token and --base-url given before `mcp` apply to every call;\n" +
			"a call may pick another tenant, never another credential.\n\n" +
			"By default only reads run. Commands that print a secret (get deploy-token,\n" +
			"get ci-config, get env --show-secrets, registry password, api GET) need\n" +
			"--allow-secrets; anything that changes state needs --allow-writes, and a write\n" +
			"that prompts still needs --yes in its args, since nobody is there to answer.\n" +
			"terminal, login, tunnel connect, watches and follows are refused: they never return.",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: flagAllowWrites, Usage: "allow commands that change state (implies --allow-secrets)"},
			&cli.BoolFlag{Name: flagAllowSecrets, Usage: "allow commands that print secrets"},
			&cli.DurationFlag{Name: flagMCPTimeout, Value: mcpDefaultTimeout, Usage: "longest a single command may run"},
		},
		Action: mcpAction,
	}
}

func mcpAction(ctx context.Context, cmd *cli.Command) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate own binary: %w", err)
	}
	s := &mcpServer{
		self:         self,
		allowWrites:  cmd.Bool(flagAllowWrites),
		allowSecrets: cmd.Bool(flagAllowSecrets) || cmd.Bool(flagAllowWrites),
		timeout:      cmd.Duration(flagMCPTimeout),
	}
	for _, f := range []string{flagConfig, flagToken, flagOrg, flagBaseURL} {
		if cmd.IsSet(f) {
			s.baseArgs = append(s.baseArgs, "--"+f, cmd.String(f))
		}
	}

	server := mcp.NewServer(
		&mcp.Implementation{Name: progName, Title: "Hamravesh Darkube", Version: version},
		&mcp.ServerOptions{Instructions: s.instructions(NewApp())},
	)
	s.register(server)
	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("mcp: %w", err)
	}
	return nil
}

// instructions tells the client what exists and what this server lets it run,
// generated from the command tree so it cannot drift from the commands.
func (s *mcpServer) instructions(root *cli.Command) string {
	var b strings.Builder
	b.WriteString("darkubectl is a kubectl-like CLI for the Hamravesh Darkube platform (PaaS on Kubernetes). ")
	b.WriteString("A tenant is an organization, a namespace is a project, an app is a Deployment with its Service and Ingress. ")
	b.WriteString("Start with `whoami` to learn the tenants, then `get apps -o wide`. ")
	b.WriteString("Prefer the named tools; the `darkubectl` tool runs any command below, and `--help` on one shows its flags. ")
	b.WriteString("Pass `-o json` when you will parse the output.\n\n")
	fmt.Fprintf(&b, "This server allows: reads%s%s.\n\nCommands (access level in brackets):\n",
		map[bool]string{true: ", secrets"}[s.allowSecrets], map[bool]string{true: ", writes"}[s.allowWrites])
	var walk func(c *cli.Command, path []string)
	walk = func(c *cli.Command, path []string) {
		for _, sub := range c.Commands {
			if sub.Hidden {
				continue
			}
			p := append(slices.Clone(path), sub.Name)
			if len(sub.Commands) > 0 {
				walk(sub, p)
				continue
			}
			a, _ := mcpInvocation{path: p, flags: map[string]bool{}}.classify()
			fmt.Fprintf(&b, "- %s [%s]: %s\n", strings.Join(p, " "), a, sub.Usage)
		}
	}
	walk(root, nil)
	return b.String()
}

type mcpRunInput struct {
	Args           []string `json:"args"                     jsonschema:"argv after darkubectl, e.g. [\"get\",\"apps\",\"-o\",\"json\"]"`
	Tenant         string   `json:"tenant,omitempty"         jsonschema:"tenant (organization slug), instead of the configured one"`
	TimeoutSeconds int      `json:"timeoutSeconds,omitempty" jsonschema:"give up after this many seconds (capped by the server's --timeout)"`
}

func (s *mcpServer) register(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name: progName,
		Description: "Run any darkubectl command and return its output. " +
			"The server's instructions list every command and whether this server allows it; " +
			"add --help to any command for its flags. Writes that prompt need --yes.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    !s.allowWrites,
			DestructiveHint: new(s.allowWrites),
			OpenWorldHint:   new(true),
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpRunInput) (*mcp.CallToolResult, any, error) {
		return s.run(ctx, in.Tenant, in.TimeoutSeconds, in.Args), nil, nil
	})
	s.registerTyped(server)
}

// check refuses an invocation this server is not allowed to run, naming the
// flag that would allow it.
func (s *mcpServer) check(args []string) error {
	if len(args) == 0 {
		return errMCPEmptyArgs
	}
	inv, err := resolveMCPInvocation(NewApp(), args)
	if err != nil {
		return err
	}
	access, why := inv.classify()
	name := strings.TrimSpace("darkubectl " + inv.key())
	switch access {
	case accessBlocked:
		if why == "" {
			why = "it needs a terminal or never returns"
		}
		return fmt.Errorf("`%s` is not available over MCP: %s", name, why)
	case accessWrite:
		if !s.allowWrites {
			return fmt.Errorf("`%s` changes state, and this server was started without --%s", name, flagAllowWrites)
		}
	case accessSecret:
		if !s.allowSecrets {
			return fmt.Errorf("`%s` can print secrets, and this server was started without --%s", name, flagAllowSecrets)
		}
	case accessRead:
	}
	return nil
}

func (s *mcpServer) run(ctx context.Context, tenant string, timeoutSeconds int, args []string) *mcp.CallToolResult {
	if err := s.check(args); err != nil {
		return mcpError(err.Error())
	}

	timeout := s.timeout
	if t := time.Duration(timeoutSeconds) * time.Second; t > 0 && t < timeout {
		timeout = t
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	argv := slices.Clone(s.baseArgs)
	if tenant != "" {
		argv = append(argv, "--"+flagOrg, tenant)
	}
	argv = append(argv, args...)

	var stdout, stderr bytes.Buffer
	//nolint:gosec // s.self is this binary, and argv has passed check.
	c := exec.CommandContext(ctx, s.self, argv...)
	// No stdin: a confirmation prompt reads EOF and declines, which is the
	// answer an absent human gives. NO_COLOR keeps escape codes out of the text.
	c.Stdin = nil
	c.Stdout = &stdout
	c.Stderr = &stderr
	c.Env = append(os.Environ(), "NO_COLOR=1")
	c.WaitDelay = mcpWaitDelay
	runErr := c.Run()

	var text strings.Builder
	text.WriteString(truncateMCP(stdout.String()))
	if stderr.Len() > 0 {
		if text.Len() > 0 {
			text.WriteString("\n")
		}
		text.WriteString("[stderr]\n")
		text.WriteString(truncateMCP(stderr.String()))
	}
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		fmt.Fprintf(&text, "\n[timed out after %s]", timeout)
	case runErr != nil:
		fmt.Fprintf(&text, "\n[%v]", runErr)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text.String()}},
		IsError: runErr != nil,
	}
}

func truncateMCP(s string) string {
	if len(s) <= mcpMaxOutput {
		return s
	}
	return s[:mcpMaxOutput] + fmt.Sprintf("\n[output truncated: %d of %d bytes shown; narrow the query]", mcpMaxOutput, len(s))
}

func mcpError(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: msg}}, IsError: true}
}
