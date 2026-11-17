// Package agentsetup plans and applies client-specific agent configuration.
package agentsetup

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Client is an agent client this package knows how to configure.
type Client string

// The supported clients.
const (
	ClientCodex    Client = "codex"
	ClientClaude   Client = "claude"
	ClientOpenCode Client = "opencode"
	ClientHermes   Client = "hermes"
)

// Scope is where a setup lands: in the current project or in the user's home.
type Scope string

// The two scopes.
const (
	ScopeProject Scope = "project"
	ScopeGlobal  Scope = "global"
)

// Config is what the user asked to set up, and for whom.
type Config struct {
	Clients      []Client
	Scope        Scope
	MCP          bool
	Skill        bool
	Instructions bool
	AllowWrites  bool
	AllowSecrets bool
}

// FileChange is one file the plan creates or rewrites. Mode is the mode the
// file ends up with: its current one when it exists, else the default for the
// scope.
type FileChange struct {
	Path   string
	Before string
	After  string
	Mode   fs.FileMode
	Exists bool
}

// Plan is the full set of changes a Config amounts to on this machine, built
// without writing anything so it can be reviewed first.
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

const (
	modePrivate    fs.FileMode = 0o600
	modeShared     fs.FileMode = 0o644
	dirModePrivate fs.FileMode = 0o700
	dirModeShared  fs.FileMode = 0o755
)

var (
	errUnsupported = errors.New("selected client does not support this setup scope")

	//go:embed instructions.md
	instructionsText string

	//go:embed skill.md
	skillText string
)

// ParseClient maps a client name as typed on the command line to a Client.
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

// Validate rejects a Config that names no client or item, or that asks a client
// for something it has no place to store.
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

// Build works out every file change cfg needs, reading the current files but
// writing nothing.
func Build(cfg Config, cwd, home string) (*Plan, error) {
	return build(cfg, cwd, home, os.Getenv)
}

// build is Build with the environment passed in: CODEX_HOME, CLAUDE_CONFIG_DIR,
// OPENCODE_CONFIG, HERMES_HOME and XDG_CONFIG_HOME each move a client's global
// files.
func build(cfg Config, cwd, home string, env func(string) string) (*Plan, error) {
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

	b := &planBuilder{cfg: cfg, root: root, home: home, env: env, changes: make(map[string]*FileChange)}
	steps := []struct {
		wanted bool
		add    func() error
	}{
		{cfg.MCP, b.addMCP},
		{cfg.Skill, b.addSkill},
		{cfg.Instructions, b.addInstructions},
	}
	for _, step := range steps {
		if !step.wanted {
			continue
		}
		if err := step.add(); err != nil {
			return nil, err
		}
	}

	changes := make([]FileChange, 0, len(b.changes))
	for _, change := range b.changes {
		changes = append(changes, *change)
	}
	slices.SortFunc(changes, func(x, y FileChange) int { return strings.Compare(x.Path, y.Path) })
	return &Plan{Config: cfg, Changes: changes}, nil
}

func (b *planBuilder) addMCP() error {
	for _, client := range b.cfg.Clients {
		path := b.mcpConfigPath(client)
		if err := b.mutate(path, targetMode(b.cfg.Scope), mcpEditor(client, mcpArgs(b.cfg))); err != nil {
			return err
		}
	}
	return nil
}

// mcpEditor returns the edit that registers the server in client's config
// format.
func mcpEditor(client Client, args []string) func(string) (string, error) {
	switch client {
	case ClientCodex:
		return func(before string) (string, error) { return mergeCodexTOML(before, args) }
	case ClientOpenCode:
		entry := map[string]any{"type": "local", keyCommand: append([]string{serverName}, args...)}
		return func(before string) (string, error) {
			return mergeJSONServer(before, []string{"mcp", "servers"}, entry)
		}
	case ClientHermes:
		return func(before string) (string, error) { return mergeYAMLServer(before, args) }
	case ClientClaude:
		fallthrough
	default:
		entry := map[string]any{keyCommand: serverName, keyArgs: args}
		return func(before string) (string, error) {
			return mergeJSONServer(before, []string{"mcpServers"}, entry)
		}
	}
}

func (b *planBuilder) addSkill() error {
	for _, client := range b.cfg.Clients {
		path := b.skillPath(client)
		if err := b.mutate(path, targetMode(b.cfg.Scope), upsertSkill); err != nil {
			return err
		}
	}
	return nil
}

func (b *planBuilder) addInstructions() error {
	if b.cfg.Scope == ScopeGlobal {
		for _, client := range b.cfg.Clients {
			path := b.globalInstructionPath(client)
			if path == "" {
				continue
			}
			if err := b.mutate(path, modePrivate, upsertManaged); err != nil {
				return err
			}
		}
		return nil
	}
	// One AGENTS.md serves every client; Claude Code reads CLAUDE.md instead, so
	// it gets an import of the shared file.
	if err := b.mutate(filepath.Join(b.root, "AGENTS.md"), modeShared, upsertManaged); err != nil {
		return err
	}
	if !slices.Contains(b.cfg.Clients, ClientClaude) {
		return nil
	}
	return b.mutate(filepath.Join(b.root, "CLAUDE.md"), modeShared, upsertClaudeBridge)
}

// DiffLines is the change as "- old" and "+ new" lines.
func (c FileChange) DiffLines() []string {
	return simpleDiff(c.Before, c.After)
}

// ReviewLines is the whole plan as text: one "create" or "update" line per
// file, followed by its diff.
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
		lines = append(lines, change.DiffLines()...)
	}
	if len(lines) == 0 {
		lines = append(lines, "No changes are needed.")
	}
	return lines
}

// Apply writes the plan. It first checks that no file moved on since the plan
// was built, then stages every file next to its target and renames them into
// place, undoing the renames already made if one fails.
func (p *Plan) Apply() error {
	if p == nil {
		return errors.New("no setup plan")
	}
	for _, change := range p.Changes {
		if err := change.verifyUnchanged(); err != nil {
			return err
		}
	}

	staged := make([]stagedFile, 0, len(p.Changes))
	discard := func() {
		for _, file := range staged {
			_ = os.Remove(file.temp)
		}
	}
	for _, change := range p.Changes {
		file, err := change.stage()
		if err != nil {
			discard()
			return err
		}
		staged = append(staged, file)
	}

	for i, file := range staged {
		if err := os.Rename(file.temp, file.target); err != nil {
			discard()
			if rollbackErr := rollbackFiles(staged[:i]); rollbackErr != nil {
				return fmt.Errorf("apply %s: %w (rollback incomplete: %w)", file.change.Path, err, rollbackErr)
			}
			return fmt.Errorf("apply %s: %w", file.change.Path, err)
		}
	}
	return nil
}

// stagedFile is a change written to a temporary file beside target, the file
// the rename will replace.
type stagedFile struct {
	change FileChange
	temp   string
	target string
}

func (c FileChange) verifyUnchanged() error {
	current, err := os.ReadFile(c.Path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if c.Exists {
			return fmt.Errorf("%s disappeared after review; rerun setup", c.Path)
		}
		return nil
	case err != nil:
		return fmt.Errorf("read %s before applying: %w", c.Path, err)
	case !c.Exists || string(current) != c.Before:
		return fmt.Errorf("%s changed after review; rerun setup", c.Path)
	}
	return nil
}

func (c FileChange) stage() (stagedFile, error) {
	target := writeTarget(c.Path)
	dir := filepath.Dir(target)
	dirMode := dirModeShared
	if c.Mode.Perm() == modePrivate {
		dirMode = dirModePrivate
	}
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return stagedFile{}, fmt.Errorf("create config directory %s: %w", dir, err)
	}
	temp, err := writeTemp(dir, ".darkubectl-agent-setup-*", c.After, c.Mode.Perm())
	if err != nil {
		return stagedFile{}, fmt.Errorf("stage %s: %w", c.Path, err)
	}
	return stagedFile{change: c, temp: temp, target: target}, nil
}

// writeTarget is the file a rename onto path must replace. Agent configs are
// often symlinks into a dotfiles repository, and renaming over the link would
// swap it for a regular file and leave the real one stale, so a link is
// followed to the file it names.
func writeTarget(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

// writeTemp writes content to a new file in dir, synced and with mode set, and
// returns its name. Nothing is left behind on failure.
func writeTemp(dir, pattern, content string, mode fs.FileMode) (string, error) {
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	name := file.Name()
	_, err = file.WriteString(content)
	if err == nil {
		err = file.Chmod(mode)
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

// rollbackFiles puts back the files already renamed into place, last first.
func rollbackFiles(done []stagedFile) error {
	var errs []error
	for _, file := range slices.Backward(done) {
		var err error
		if file.change.Exists {
			err = restoreFile(file.target, file.change.Before, file.change.Mode.Perm())
		} else if err = os.Remove(file.target); errors.Is(err, fs.ErrNotExist) {
			err = nil
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", file.change.Path, err))
		}
	}
	return errors.Join(errs...)
}

func restoreFile(path, content string, mode fs.FileMode) error {
	temp, err := writeTemp(filepath.Dir(path), ".darkubectl-agent-rollback-*", content, mode)
	if err != nil {
		return err
	}
	if err := os.Rename(temp, path); err != nil {
		_ = os.Remove(temp)
		return err
	}
	return nil
}

// planBuilder accumulates the changes of one Build, keyed by path so that two
// edits of the same file compose into one change.
type planBuilder struct {
	cfg     Config
	root    string
	home    string
	env     func(string) string
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
		data, readErr := os.ReadFile(path) //nolint:gosec // the path is an agent config file this command is asked to edit
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
		return modePrivate
	}
	return modeShared
}

func projectRoot(cwd string) (string, error) {
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	for current := abs; ; current = filepath.Dir(current) {
		//nolint:gosec // walks up from the working directory looking for the repository root
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

func (b *planBuilder) mcpConfigPath(client Client) string {
	root, home := b.root, b.home
	if b.cfg.Scope == ScopeProject {
		switch client {
		case ClientCodex:
			return filepath.Join(root, ".codex", "config.toml")
		case ClientClaude:
			return filepath.Join(root, ".mcp.json")
		case ClientOpenCode:
			return openCodeConfigPath(root)
		case ClientHermes:
			// Hermes has no project config; Validate refuses the combination.
		}
	}
	switch client {
	case ClientCodex:
		return filepath.Join(b.envOr("CODEX_HOME", filepath.Join(home, ".codex")), "config.toml")
	case ClientClaude:
		return filepath.Join(home, ".claude.json")
	case ClientOpenCode:
		if path := b.env("OPENCODE_CONFIG"); path != "" {
			return path
		}
		return openCodeConfigPath(b.openCodeConfigDir())
	case ClientHermes:
		return filepath.Join(b.envOr("HERMES_HOME", filepath.Join(home, ".hermes")), "config.yaml")
	default:
		return ""
	}
}

func (b *planBuilder) skillPath(client Client) string {
	root, home := b.root, b.home
	if b.cfg.Scope == ScopeProject {
		if client == ClientClaude {
			return filepath.Join(root, ".claude", "skills", "darkubectl", "SKILL.md")
		}
		return filepath.Join(root, ".agents", "skills", "darkubectl", "SKILL.md")
	}
	switch client {
	case ClientCodex:
		return filepath.Join(b.envOr("CODEX_HOME", filepath.Join(home, ".codex")), "skills", "darkubectl", "SKILL.md")
	case ClientClaude:
		return filepath.Join(b.envOr("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude")), "skills", "darkubectl", "SKILL.md")
	case ClientOpenCode:
		return filepath.Join(b.openCodeConfigDir(), "skills", "darkubectl", "SKILL.md")
	case ClientHermes:
		return filepath.Join(b.envOr("HERMES_HOME", filepath.Join(home, ".hermes")), "skills", "darkubectl", "SKILL.md")
	default:
		return ""
	}
}

func (b *planBuilder) globalInstructionPath(client Client) string {
	home := b.home
	switch client {
	case ClientCodex:
		return filepath.Join(b.envOr("CODEX_HOME", filepath.Join(home, ".codex")), "AGENTS.md")
	case ClientClaude:
		return filepath.Join(b.envOr("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude")), "CLAUDE.md")
	case ClientOpenCode:
		return filepath.Join(b.openCodeConfigDir(), "AGENTS.md")
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

func (b *planBuilder) openCodeConfigDir() string {
	home := b.home
	if path := b.env("OPENCODE_CONFIG"); path != "" {
		return filepath.Dir(path)
	}
	if path := b.env("XDG_CONFIG_HOME"); path != "" {
		return filepath.Join(path, "opencode")
	}
	return filepath.Join(home, ".config", "opencode")
}

func (b *planBuilder) envOr(key, fallback string) string {
	if value := b.env(key); value != "" {
		return value
	}
	return fallback
}

// upsertManaged adds the instructions to a file between two markers, or
// refreshes the block already there, leaving the rest of the file alone.
func upsertManaged(before string) (string, error) {
	return upsertBlock(before, instructionStart, instructionEnd, strings.TrimSpace(instructionsText), "instruction")
}

// upsertClaudeBridge makes CLAUDE.md import AGENTS.md, the same way.
func upsertClaudeBridge(before string) (string, error) {
	return upsertBlock(before, claudeBridgeStart, claudeBridgeEnd, "@AGENTS.md", "Claude import")
}

// upsertBlock replaces the text between the start and end markers with body, or
// appends a new marked block when the file has none.
func upsertBlock(before, startMarker, endMarker, body, what string) (string, error) {
	start := strings.Index(before, startMarker)
	end := strings.Index(before, endMarker)
	if (start >= 0) != (end >= 0) || (start >= 0 && end < start) {
		return "", fmt.Errorf("managed %s markers are incomplete or out of order", what)
	}
	block := startMarker + "\n" + body + "\n" + endMarker
	if start >= 0 {
		return before[:start] + block + before[end+len(endMarker):], nil
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

// upsertSkill writes the skill file, refusing to overwrite one this command
// did not put there.
func upsertSkill(before string) (string, error) {
	if before != "" && !strings.Contains(before, skillMarker) {
		return "", errors.New("a skill already exists at this path and is not managed by darkubectl; move it or choose another skill name")
	}
	return skillText, nil
}

// simpleDiff shows the lines that differ between the common head and tail of
// two texts: enough for edits that touch one block of a file.
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
	removed, added := oldLines[prefix:len(oldLines)-suffix], newLines[prefix:len(newLines)-suffix]
	lines := make([]string, 0, len(removed)+len(added))
	for _, line := range removed {
		lines = append(lines, "- "+line)
	}
	for _, line := range added {
		lines = append(lines, "+ "+line)
	}
	return lines
}
