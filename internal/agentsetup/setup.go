// Package agentsetup plans and applies client-specific agent configuration.
package agentsetup

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Client string

const (
	ClientCodex    Client = "codex"
	ClientClaude   Client = "claude"
	ClientOpenCode Client = "opencode"
	ClientHermes   Client = "hermes"
)

type Scope string

const (
	ScopeProject Scope = "project"
	ScopeGlobal  Scope = "global"
)

type Config struct {
	Clients      []Client
	Scope        Scope
	MCP          bool
	Skill        bool
	Instructions bool
	AllowWrites  bool
	AllowSecrets bool
}

type FileChange struct {
	Path   string
	Before string
	After  string
	Mode   fs.FileMode
	Exists bool
}

type Plan struct {
	Config  Config
	Changes []FileChange
}

const (
	instructionStart  = "<!-- darkubectl-agent-setup:start -->"
	instructionEnd    = "<!-- darkubectl-agent-setup:end -->"
	skillMarker       = "<!-- darkubectl-agent-setup:skill -->"
	claudeBridgeStart = "<!-- darkubectl-agent-setup:claude-start -->"
	claudeBridgeEnd   = "<!-- darkubectl-agent-setup:claude-end -->"
)

var errUnsupported = errors.New("selected client does not support this setup scope")

func ParseClient(s string) (Client, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "codex":
		return ClientCodex, nil
	case "claude", "claude-code":
		return ClientClaude, nil
	case "opencode", "open-code":
		return ClientOpenCode, nil
	case "hermes":
		return ClientHermes, nil
	default:
		return "", fmt.Errorf("unknown agent client %q (choose codex, claude, opencode, or hermes)", s)
	}
}

func Validate(cfg Config) error {
	if len(cfg.Clients) == 0 {
		return errors.New("select at least one agent client")
	}
	if cfg.Scope != ScopeProject && cfg.Scope != ScopeGlobal {
		return errors.New("scope must be project or global")
	}
	if !cfg.MCP && !cfg.Skill && !cfg.Instructions {
		return errors.New("select at least one setup item: --mcp, --skill, or --instructions")
	}
	if (cfg.AllowWrites || cfg.AllowSecrets) && !cfg.MCP {
		return errors.New("--allow-writes and --allow-secrets require --mcp")
	}
	seen := make(map[Client]bool, len(cfg.Clients))
	for _, client := range cfg.Clients {
		switch client {
		case ClientCodex, ClientClaude, ClientOpenCode, ClientHermes:
		default:
			return fmt.Errorf("unsupported agent client %q", client)
		}
		if seen[client] {
			return fmt.Errorf("agent client %q was selected more than once", client)
		}
		seen[client] = true
	}
	if cfg.Scope == ScopeProject && cfg.MCP && seen[ClientHermes] {
		return fmt.Errorf("%w: Hermes stores MCP servers in global config; choose global scope or deselect Hermes", errUnsupported)
	}
	if cfg.Scope == ScopeGlobal && cfg.Instructions && seen[ClientHermes] {
		return fmt.Errorf("%w: Hermes has no neutral global instructions file; choose project scope or deselect Hermes", errUnsupported)
	}
	return nil
}

func Build(cfg Config, cwd, home string) (*Plan, error) {
	if err := Validate(cfg); err != nil {
		return nil, err
	}
	root, err := projectRoot(cwd)
	if err != nil {
		return nil, err
	}
	if home == "" {
		home, err = os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("find home directory: %w", err)
		}
	}
	home, err = filepath.Abs(home)
	if err != nil {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}

	b := &planBuilder{changes: make(map[string]*FileChange)}
	if cfg.MCP {
		for _, client := range cfg.Clients {
			path := mcpConfigPath(client, cfg.Scope, root, home)
			switch client {
			case ClientCodex:
				if err := b.mutate(path, targetMode(cfg.Scope), func(before string) (string, error) {
					return mergeCodexTOML(before, mcpArgs(cfg))
				}); err != nil {
					return nil, err
				}
			case ClientClaude:
				entry := map[string]any{"command": "darkubectl", "args": anyStrings(mcpArgs(cfg))}
				if err := b.mutate(path, targetMode(cfg.Scope), func(before string) (string, error) {
					return mergeJSONServer(before, []string{"mcpServers"}, entry)
				}); err != nil {
					return nil, err
				}
			case ClientOpenCode:
				command := append([]string{"darkubectl"}, mcpArgs(cfg)...)
				entry := map[string]any{"type": "local", "command": anyStrings(command)}
				if err := b.mutate(path, targetMode(cfg.Scope), func(before string) (string, error) {
					return mergeJSONServer(before, []string{"mcp", "servers"}, entry)
				}); err != nil {
					return nil, err
				}
			case ClientHermes:
				entry := map[string]any{"command": "darkubectl", "args": anyStrings(mcpArgs(cfg))}
				if err := b.mutate(path, targetMode(cfg.Scope), func(before string) (string, error) {
					return mergeYAMLServer(before, entry)
				}); err != nil {
					return nil, err
				}
			}
		}
	}

	if cfg.Skill {
		content := darkubectlSkill()
		for _, client := range cfg.Clients {
			path := skillPath(client, cfg.Scope, root, home)
			if err := b.mutate(path, targetMode(cfg.Scope), func(before string) (string, error) {
				return upsertSkill(before, content)
			}); err != nil {
				return nil, err
			}
		}
	}

	if cfg.Instructions {
		content := darkubectlInstructions()
		if cfg.Scope == ScopeProject {
			path := filepath.Join(root, "AGENTS.md")
			if err := b.mutate(path, 0o644, func(before string) (string, error) {
				return upsertManaged(before, content)
			}); err != nil {
				return nil, err
			}
			for _, client := range cfg.Clients {
				if client != ClientClaude {
					continue
				}
				claudePath := filepath.Join(root, "CLAUDE.md")
				if err := b.mutate(claudePath, 0o644, func(before string) (string, error) {
					return upsertClaudeBridge(before)
				}); err != nil {
					return nil, err
				}
			}
		} else {
			for _, client := range cfg.Clients {
				path := globalInstructionPath(client, home)
				if path == "" {
					continue
				}
				if err := b.mutate(path, 0o600, func(before string) (string, error) {
					return upsertManaged(before, content)
				}); err != nil {
					return nil, err
				}
			}
		}
	}

	changes := make([]FileChange, 0, len(b.changes))
	for _, change := range b.changes {
		changes = append(changes, *change)
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return &Plan{Config: cfg, Changes: changes}, nil
}

func (p *Plan) ReviewLines() []string {
	if p == nil {
		return nil
	}
	var lines []string
	for _, change := range p.Changes {
		action := "update"
		if !change.Exists {
			action = "create"
		}
		lines = append(lines, fmt.Sprintf("%s %s", action, change.Path))
		lines = append(lines, simpleDiff(change.Before, change.After)...)
	}
	if len(lines) == 0 {
		lines = append(lines, "No changes are needed.")
	}
	return lines
}

func (p *Plan) Apply() error {
	if p == nil {
		return errors.New("no setup plan")
	}
	if len(p.Changes) == 0 {
		return nil
	}
	for _, change := range p.Changes {
		current, err := os.ReadFile(change.Path)
		if errors.Is(err, fs.ErrNotExist) {
			if change.Exists {
				return fmt.Errorf("%s disappeared after review; rerun setup", change.Path)
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("read %s before applying: %w", change.Path, err)
		}
		if !change.Exists || string(current) != change.Before {
			return fmt.Errorf("%s changed after review; rerun setup", change.Path)
		}
	}

	staged := make([]stagedFile, 0, len(p.Changes))
	cleanup := func() {
		for _, file := range staged {
			_ = os.Remove(file.temp)
		}
	}
	for _, change := range p.Changes {
		dir := filepath.Dir(change.Path)
		dirMode := fs.FileMode(0o755)
		if change.Mode.Perm() == 0o600 {
			dirMode = 0o700
		}
		if err := os.MkdirAll(dir, dirMode); err != nil {
			cleanup()
			return fmt.Errorf("create config directory %s: %w", dir, err)
		}
		file, err := os.CreateTemp(dir, ".darkubectl-agent-setup-*")
		if err != nil {
			cleanup()
			return fmt.Errorf("stage %s: %w", change.Path, err)
		}
		if _, err := file.WriteString(change.After); err != nil {
			_ = file.Close()
			_ = os.Remove(file.Name())
			cleanup()
			return fmt.Errorf("stage %s: %w", change.Path, err)
		}
		if err := file.Chmod(change.Mode.Perm()); err != nil {
			_ = file.Close()
			_ = os.Remove(file.Name())
			cleanup()
			return fmt.Errorf("set permissions on staged %s: %w", change.Path, err)
		}
		if err := file.Sync(); err != nil {
			_ = file.Close()
			_ = os.Remove(file.Name())
			cleanup()
			return fmt.Errorf("flush staged %s: %w", change.Path, err)
		}
		if err := file.Close(); err != nil {
			_ = os.Remove(file.Name())
			cleanup()
			return fmt.Errorf("close staged %s: %w", change.Path, err)
		}
		staged = append(staged, stagedFile{change: change, temp: file.Name()})
	}

	committed := make([]FileChange, 0, len(staged))
	for _, file := range staged {
		if err := os.Rename(file.temp, file.change.Path); err != nil {
			cleanup()
			if rollbackErr := rollbackFiles(committed); rollbackErr != nil {
				return fmt.Errorf("apply %s: %w (rollback incomplete: %v)", file.change.Path, err, rollbackErr)
			}
			return fmt.Errorf("apply %s: %w", file.change.Path, err)
		}
		committed = append(committed, file.change)
	}
	return nil
}

type stagedFile struct {
	change FileChange
	temp   string
}

func rollbackFiles(changes []FileChange) error {
	var errs []string
	for i := len(changes) - 1; i >= 0; i-- {
		change := changes[i]
		if !change.Exists {
			if err := os.Remove(change.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				errs = append(errs, fmt.Sprintf("%s: %v", change.Path, err))
			}
			continue
		}
		if err := atomicWrite(change.Path, change.Before, change.Mode.Perm()); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", change.Path, err))
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func atomicWrite(path, content string, mode fs.FileMode) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".darkubectl-agent-rollback-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.WriteString(content); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

type planBuilder struct {
	changes map[string]*FileChange
}

func (b *planBuilder) mutate(path string, mode fs.FileMode, edit func(string) (string, error)) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve target path: %w", err)
	}
	current := ""
	exists := false
	if change, ok := b.changes[path]; ok {
		current = change.After
		exists = change.Exists
		mode = change.Mode
	} else {
		data, readErr := os.ReadFile(path)
		switch {
		case readErr == nil:
			current, exists = string(data), true
			info, infoErr := os.Stat(path)
			if infoErr != nil {
				return fmt.Errorf("stat %s: %w", path, infoErr)
			}
			mode = info.Mode().Perm()
		case errors.Is(readErr, fs.ErrNotExist):
			// New global config files are private; project files remain readable.
		default:
			return fmt.Errorf("read %s: %w", path, readErr)
		}
	}
	next, err := edit(current)
	if err != nil {
		return fmt.Errorf("prepare %s: %w", path, err)
	}
	if next == current {
		return nil
	}
	b.changes[path] = &FileChange{Path: path, Before: current, After: next, Mode: mode, Exists: exists}
	return nil
}

func mcpArgs(cfg Config) []string {
	args := []string{"mcp"}
	if cfg.AllowWrites {
		args = append(args, "--allow-writes")
	} else if cfg.AllowSecrets {
		args = append(args, "--allow-secrets")
	}
	return args
}

func targetMode(scope Scope) fs.FileMode {
	if scope == ScopeGlobal {
		return 0o600
	}
	return 0o644
}

func projectRoot(cwd string) (string, error) {
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	for current := abs; ; current = filepath.Dir(current) {
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			return current, nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("inspect project root %s: %w", current, err)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return abs, nil
		}
	}
}

func mcpConfigPath(client Client, scope Scope, root, home string) string {
	if scope == ScopeProject {
		switch client {
		case ClientCodex:
			return filepath.Join(root, ".codex", "config.toml")
		case ClientClaude:
			return filepath.Join(root, ".mcp.json")
		case ClientOpenCode:
			return openCodeConfigPath(root)
		}
	}
	switch client {
	case ClientCodex:
		return filepath.Join(envOr("CODEX_HOME", filepath.Join(home, ".codex")), "config.toml")
	case ClientClaude:
		return filepath.Join(home, ".claude.json")
	case ClientOpenCode:
		if path := os.Getenv("OPENCODE_CONFIG"); path != "" {
			return path
		}
		return openCodeConfigPath(openCodeConfigDir(home))
	case ClientHermes:
		return filepath.Join(envOr("HERMES_HOME", filepath.Join(home, ".hermes")), "config.yaml")
	default:
		return ""
	}
}

func skillPath(client Client, scope Scope, root, home string) string {
	if scope == ScopeProject {
		if client == ClientClaude {
			return filepath.Join(root, ".claude", "skills", "darkubectl", "SKILL.md")
		}
		return filepath.Join(root, ".agents", "skills", "darkubectl", "SKILL.md")
	}
	switch client {
	case ClientCodex:
		return filepath.Join(envOr("CODEX_HOME", filepath.Join(home, ".codex")), "skills", "darkubectl", "SKILL.md")
	case ClientClaude:
		return filepath.Join(envOr("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude")), "skills", "darkubectl", "SKILL.md")
	case ClientOpenCode:
		return filepath.Join(openCodeConfigDir(home), "skills", "darkubectl", "SKILL.md")
	case ClientHermes:
		return filepath.Join(envOr("HERMES_HOME", filepath.Join(home, ".hermes")), "skills", "darkubectl", "SKILL.md")
	default:
		return ""
	}
}

func globalInstructionPath(client Client, home string) string {
	switch client {
	case ClientCodex:
		return filepath.Join(envOr("CODEX_HOME", filepath.Join(home, ".codex")), "AGENTS.md")
	case ClientClaude:
		return filepath.Join(envOr("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude")), "CLAUDE.md")
	case ClientOpenCode:
		return filepath.Join(openCodeConfigDir(home), "AGENTS.md")
	default:
		return ""
	}
}

func openCodeConfigPath(base string) string {
	for _, name := range []string{"opencode.json", "opencode.jsonc"} {
		path := filepath.Join(base, name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return filepath.Join(base, "opencode.json")
}

func openCodeConfigDir(home string) string {
	if path := os.Getenv("OPENCODE_CONFIG"); path != "" {
		return filepath.Dir(path)
	}
	if path := os.Getenv("XDG_CONFIG_HOME"); path != "" {
		return filepath.Join(path, "opencode")
	}
	return filepath.Join(home, ".config", "opencode")
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func anyStrings(items []string) []any {
	out := make([]any, len(items))
	for i, item := range items {
		out[i] = item
	}
	return out
}

func upsertManaged(before, content string) (string, error) {
	start := strings.Index(before, instructionStart)
	end := strings.Index(before, instructionEnd)
	if (start >= 0) != (end >= 0) || (start >= 0 && end < start) {
		return "", errors.New("managed instruction markers are incomplete or out of order")
	}
	block := instructionStart + "\n" + strings.TrimSpace(content) + "\n" + instructionEnd
	if start >= 0 {
		end += len(instructionEnd)
		return before[:start] + block + before[end:], nil
	}
	if before == "" {
		return block + "\n", nil
	}
	separator := "\n"
	if !strings.HasSuffix(before, "\n") {
		separator = "\n\n"
	}
	return before + separator + block + "\n", nil
}

func upsertClaudeBridge(before string) (string, error) {
	start := strings.Index(before, claudeBridgeStart)
	end := strings.Index(before, claudeBridgeEnd)
	if (start >= 0) != (end >= 0) || (start >= 0 && end < start) {
		return "", errors.New("managed Claude import markers are incomplete or out of order")
	}
	block := claudeBridgeStart + "\n@AGENTS.md\n" + claudeBridgeEnd
	if start >= 0 {
		end += len(claudeBridgeEnd)
		return before[:start] + block + before[end:], nil
	}
	if before == "" {
		return block + "\n", nil
	}
	separator := "\n"
	if !strings.HasSuffix(before, "\n") {
		separator = "\n\n"
	}
	return before + separator + block + "\n", nil
}

func upsertSkill(before, content string) (string, error) {
	if before != "" && !strings.Contains(before, skillMarker) {
		return "", errors.New("a skill already exists at this path and is not managed by darkubectl; move it or choose another skill name")
	}
	return content, nil
}

func darkubectlInstructions() string {
	return strings.Join([]string{
		"## Darkubectl",
		"- Darkube uses tenant for organization, namespace for project, and app for a managed application. Never guess the tenant.",
		"- When the account, membership, or tenant is unclear, call whoami. Prefer a matching typed MCP tool; if none fits, use the generic darkubectl MCP tool. Use the CLI only when MCP is unavailable or does not expose the operation.",
		"- Typed MCP tools may select a tenant for one call, but all calls keep the server's local configuration, credential, and API endpoint. Do not switch to the CLI to get around an MCP access denial.",
		"- Inspect current state before making a requested change. Use dryRun where supported, review the target and proposed change, then read the affected resource back; an accepted request or HTTP 202 alone does not prove it persisted.",
		"- Follow the MCP server's tool schemas and access instructions. Reads are enabled by default; secret reads require --allow-secrets and writes require --allow-writes, which also enables secret access. Never work around denied or blocked operations.",
		"- Only pass --yes for a user-requested change after checking its exact target; it bypasses interactive prompts.",
		"- Treat API keys, tokens, and secret environment values as sensitive. Do not expose them in chat, logs, or generated files; access a secret only when the user's request requires it and the configured MCP access permits it.",
		"- Do not use delete as routine cleanup or a name-conflict fix. If app deletion is requested, verify the exact tenant and app; deletion is asynchronous and may leave an orphaned Helm release. Check get orphans before recreating an app with a blocked name.",
		"- Creating apps or namespaces and opening pod terminals require a Console JWT; an account API key cannot perform them. Namespace creation also requires a name prefixed by the tenant slug, and deleting a populated namespace removes its workloads. Explain credential or target constraints before proceeding.",
		"- Use Darkubectl's schemas, help, and repository guidance for platform behavior. Do not infer unsupported Darkube behavior from Kubernetes conventions.",
	}, "\n")
}

func darkubectlSkill() string {
	return strings.Join([]string{
		"---",
		"name: darkubectl",
		"description: Safely inspect and manage Hamravesh Darkube tenants, apps, builds, domains, and support data with darkubectl MCP tools or CLI.",
		"---",
		"",
		skillMarker,
		"# Darkubectl",
		"",
		"Use this skill for Hamravesh Darkube operations. Prefer the platform's typed MCP tools because their schemas describe supported arguments. If none fits, use the generic darkubectl MCP command tool. Use the CLI only when MCP is unavailable or does not expose the operation. Never switch paths to bypass an MCP denial or access policy.",
		"",
		"## Platform model",
		"",
		"Darkube is Hamravesh's application platform on Kubernetes. Its API calls are usually tenant-scoped: an organization is a tenant, a namespace is a project, and an app is a managed application. Never guess which tenant the user means.",
		"",
		"## Workflow",
		"",
		"1. If the account, membership, or tenant is unclear, call whoami and use its tenant list. Typed tools accept an optional tenant to select the organization for that call; this does not change the server's credential, config, or API endpoint.",
		"2. Use the most specific tool available, such as list_apps, describe_app, get_app_spec, get_pods, app_logs, get_domains, or rollout_status. Use the generic darkubectl tool only for commands without a typed tool; pass its args as an argument list, not shell text.",
		"3. Follow the MCP server's command access instructions. The generic tool applies policy to the resolved command; commands not classified as reads require write access, and terminal or non-returning commands remain blocked. A CLI path is not a way around an MCP denial.",
		"4. Inspect the current resource before proposing or making a change. Use JSON or YAML output when parsing CLI results. Check schemas, command help, or repository guidance instead of inferring behavior from Kubernetes conventions.",
		"5. Only make changes the user requested. Use dryRun when supported and review the target and diff before writing. Read the resource back afterward; a successful request or HTTP 202 response alone does not prove the platform stored the change.",
		"",
		"## Access and credentials",
		"",
		"The MCP server uses this machine's darkubectl configuration, credential, and API endpoint for every call. The optional tenant parameter on typed tools selects the organization for that call only.",
		"",
		"MCP is read-only by default. The generic command tool permits reads unless a command may expose secrets; secret operations require --allow-secrets. Write tools are registered only when the server starts with --allow-writes, which also enables secret access. Commands needing a terminal or that run indefinitely remain blocked. Never bypass a denial by changing tools, credentials, or execution paths.",
		"",
		"The MCP child has no interactive stdin. A generic command that prompts needs --yes; only include it for a user-requested change after checking its exact target.",
		"",
		"Treat API keys, secret environment values, registry credentials, and deployment tokens as sensitive. Secret environment values are hidden by default. Do not print, log, or copy secrets into generated files; only pass or reveal a secret when the user's explicit task requires it and the configured MCP access permits it.",
		"",
		"An account API key cannot create apps or namespaces or open pod terminals; those operations require a Console JWT. Namespace creation also requires its name to start with the tenant slug. If the available credential cannot perform the requested operation, explain the requirement instead of trying another credential path.",
		"",
		"## Deletion and recovery",
		"",
		"Do not use delete as routine cleanup or as a fix for an app-name conflict. If the user requests deletion, verify the exact tenant and target first. App deletion is asynchronous and may leave its Helm release behind; use get orphans to inspect releases before recreating an app whose name is blocked, and report only the state darkubectl confirms.",
		"",
		"Namespace deletion is destructive: a populated namespace's workloads are removed with it. Inspect its apps first and do not rely on the API to protect them. Namespace deletion requires a Console JWT.",
		"",
	}, "\n")
}

func simpleDiff(before, after string) []string {
	oldLines := strings.Split(strings.TrimSuffix(before, "\n"), "\n")
	newLines := strings.Split(strings.TrimSuffix(after, "\n"), "\n")
	if before == "" {
		oldLines = nil
	}
	if after == "" {
		newLines = nil
	}
	prefix := 0
	for prefix < len(oldLines) && prefix < len(newLines) && oldLines[prefix] == newLines[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(oldLines)-prefix && suffix < len(newLines)-prefix &&
		oldLines[len(oldLines)-1-suffix] == newLines[len(newLines)-1-suffix] {
		suffix++
	}
	var lines []string
	for _, line := range oldLines[prefix : len(oldLines)-suffix] {
		lines = append(lines, "- "+line)
	}
	for _, line := range newLines[prefix : len(newLines)-suffix] {
		lines = append(lines, "+ "+line)
	}
	return lines
}
