package agentsetup

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// noEnv is an environment with nothing set, so a developer's own CODEX_HOME or
// XDG_CONFIG_HOME cannot move the files a test expects.
func noEnv(string) string { return "" }

// sandbox gives a test its own project (a git repository root) and home.
func sandbox(t *testing.T) (string, string) {
	t.Helper()
	// macOS temp directories sit behind a symlink; resolve it so paths compare.
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temp dir: %v", err)
	}
	project, home := filepath.Join(base, "project"), filepath.Join(base, "home")
	for _, dir := range []string{filepath.Join(project, ".git"), filepath.Join(project, "sub"), home} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	return project, home
}

func mustBuild(t *testing.T, cfg Config, cwd, home string) *Plan {
	t.Helper()
	plan, err := build(cfg, cwd, home, noEnv)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return plan
}

func planPaths(plan *Plan, base string) string {
	paths := make([]string, 0, len(plan.Changes))
	for _, change := range plan.Changes {
		rel, err := filepath.Rel(base, change.Path)
		if err != nil {
			rel = change.Path
		}
		paths = append(paths, filepath.ToSlash(rel))
	}
	return strings.Join(paths, " ")
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // a path inside the test's temp dir
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(data)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestProjectSetupFromSubdirectory(t *testing.T) {
	t.Parallel()

	project, home := sandbox(t)
	cfg := Config{
		Clients: []Client{ClientCodex, ClientClaude, ClientOpenCode},
		Scope:   ScopeProject, MCP: true, Skill: true, Instructions: true,
	}

	// Run from a subdirectory: everything still lands at the repository root.
	plan := mustBuild(t, cfg, filepath.Join(project, "sub"), home)
	want := ".agents/skills/darkubectl/SKILL.md .claude/skills/darkubectl/SKILL.md " +
		".codex/config.toml .mcp.json AGENTS.md CLAUDE.md opencode.json"
	if got := planPaths(plan, project); got != want {
		t.Fatalf("planned files:\n got %s\nwant %s", got, want)
	}
	if _, err := os.Stat(filepath.Join(project, "AGENTS.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Build wrote a file before Apply: %v", err)
	}
	review := strings.Join(plan.ReviewLines(), "\n")
	if !strings.Contains(review, "create "+filepath.Join(project, ".mcp.json")) || !strings.Contains(review, `+     "darkubectl": {`) {
		t.Errorf("review does not show the new MCP config:\n%s", review)
	}

	if err := plan.Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := readFile(t, filepath.Join(project, "CLAUDE.md")); !strings.Contains(got, "@AGENTS.md") {
		t.Errorf("CLAUDE.md does not import AGENTS.md:\n%s", got)
	}
	if got := readFile(t, filepath.Join(project, ".codex", "config.toml")); !strings.Contains(got, `args = ["mcp"]`) {
		t.Errorf("codex config is read-only by default, got:\n%s", got)
	}
	if left, _ := filepath.Glob(filepath.Join(project, ".darkubectl-agent-*")); len(left) != 0 {
		t.Errorf("staging files left behind: %v", left)
	}

	// The same request again is a no-op.
	if again := mustBuild(t, cfg, project, home); len(again.Changes) != 0 {
		t.Fatalf("second run still wants changes:\n%s", strings.Join(again.ReviewLines(), "\n"))
	}

	// Granting writes later edits the three MCP configs and nothing else.
	cfg.AllowWrites = true
	upgrade := mustBuild(t, cfg, project, home)
	if got := planPaths(upgrade, project); got != ".codex/config.toml .mcp.json opencode.json" {
		t.Fatalf("upgrade touches: %s", got)
	}
	for _, change := range upgrade.Changes {
		if !change.Exists || !strings.Contains(change.After, "--allow-writes") {
			t.Errorf("%s: want an update adding --allow-writes, got:\n%s", change.Path, change.After)
		}
	}
}

func TestGlobalSetupIsPrivate(t *testing.T) {
	t.Parallel()

	project, home := sandbox(t)
	cfg := Config{
		Clients: []Client{ClientCodex, ClientClaude, ClientOpenCode, ClientHermes},
		Scope:   ScopeGlobal, MCP: true, Skill: true, AllowSecrets: true,
	}
	plan := mustBuild(t, cfg, project, home)
	want := ".claude.json .claude/skills/darkubectl/SKILL.md .codex/config.toml .codex/skills/darkubectl/SKILL.md " +
		".config/opencode/opencode.json .config/opencode/skills/darkubectl/SKILL.md .hermes/config.yaml .hermes/skills/darkubectl/SKILL.md"
	if got := planPaths(plan, home); got != want {
		t.Fatalf("planned files:\n got %s\nwant %s", got, want)
	}
	if err := plan.Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	info, err := os.Stat(filepath.Join(home, ".hermes", "config.yaml"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != modePrivate {
		t.Errorf("a new global config has mode %v, want %v", info.Mode().Perm(), modePrivate)
	}
	if got := readFile(t, filepath.Join(home, ".claude.json")); !strings.Contains(got, `"--allow-secrets"`) {
		t.Errorf("secret access was not configured:\n%s", got)
	}
}

func TestEnvironmentRelocatesGlobalConfig(t *testing.T) {
	t.Parallel()

	project, home := sandbox(t)
	codexHome := filepath.Join(home, "elsewhere")
	env := func(key string) string {
		if key == "CODEX_HOME" {
			return codexHome
		}
		return ""
	}

	plan, err := build(Config{Clients: []Client{ClientCodex}, Scope: ScopeGlobal, MCP: true}, project, home, env)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(plan.Changes) != 1 || plan.Changes[0].Path != filepath.Join(codexHome, "config.toml") {
		t.Fatalf("CODEX_HOME was ignored: %s", planPaths(plan, home))
	}
}

// An agent config is often a symlink into a dotfiles repository. Applying must
// write through the link: replacing it with a regular file would leave the
// repository copy stale and the link gone.
func TestApplyWritesThroughSymlink(t *testing.T) {
	t.Parallel()

	project, home := sandbox(t)
	target := filepath.Join(home, "dotfiles", "CLAUDE.md")
	writeFile(t, target, "# mine\n")
	link := filepath.Join(home, ".claude", "CLAUDE.md")
	if err := os.MkdirAll(filepath.Dir(link), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	plan := mustBuild(t, Config{Clients: []Client{ClientClaude}, Scope: ScopeGlobal, Instructions: true}, project, home)
	if err := plan.Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the symlink was replaced by a regular file")
	}
	if got := readFile(t, target); !strings.HasPrefix(got, "# mine\n") || !strings.Contains(got, instructionStart) {
		t.Fatalf("the linked file was not updated:\n%s", got)
	}
}

func TestApplyRefusesAFileThatChangedAfterReview(t *testing.T) {
	t.Parallel()

	project, home := sandbox(t)
	agents := filepath.Join(project, "AGENTS.md")
	writeFile(t, agents, "# Project\n")
	cfg := Config{Clients: []Client{ClientCodex}, Scope: ScopeProject, MCP: true, Instructions: true}

	plan := mustBuild(t, cfg, project, home)
	writeFile(t, agents, "# Project, edited meanwhile\n")
	if err := plan.Apply(); err == nil || !strings.Contains(err.Error(), "changed after review") {
		t.Fatalf("want a changed-after-review error, got %v", err)
	}
	// The check runs before any write, so the other file was not created.
	if _, err := os.Stat(filepath.Join(project, ".codex", "config.toml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a file was written despite the refusal: %v", err)
	}

	// The same holds for a file that appears where the plan meant to create one.
	plan = mustBuild(t, cfg, project, home)
	writeFile(t, filepath.Join(project, ".codex", "config.toml"), "model = \"x\"\n")
	if err := plan.Apply(); err == nil {
		t.Fatal("a file created after review was overwritten")
	}
}

func TestRollbackRestoresEveryFile(t *testing.T) {
	t.Parallel()

	project, home := sandbox(t)
	agents := filepath.Join(project, "AGENTS.md")
	writeFile(t, agents, "# Project\n")
	plan := mustBuild(t, Config{Clients: []Client{ClientCodex}, Scope: ScopeProject, MCP: true, Instructions: true}, project, home)

	// Put both changes in place the way Apply does, then undo them as Apply
	// would after a later rename failed: the created file goes, the updated one
	// gets its old content back.
	staged := make([]stagedFile, 0, len(plan.Changes))
	for _, change := range plan.Changes {
		file, err := change.stage()
		if err != nil {
			t.Fatalf("stage: %v", err)
		}
		staged = append(staged, file)
	}
	if err := os.Rename(staged[0].temp, staged[0].target); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if err := os.Rename(staged[1].temp, staged[1].target); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if err := rollbackFiles(staged); err != nil {
		t.Fatalf("rollbackFiles: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".codex", "config.toml")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a created file survived the rollback: %v", err)
	}
	if got := readFile(t, agents); got != "# Project\n" {
		t.Errorf("an updated file was not restored:\n%s", got)
	}
}

func TestBuildRefusesAForeignSkill(t *testing.T) {
	t.Parallel()

	project, home := sandbox(t)
	writeFile(t, filepath.Join(project, ".claude", "skills", "darkubectl", "SKILL.md"), "# hand-written\n")
	_, err := build(Config{Clients: []Client{ClientClaude}, Scope: ScopeProject, Skill: true}, project, home, noEnv)
	if err == nil || !strings.Contains(err.Error(), "not managed by darkubectl") {
		t.Fatalf("want a refusal, got %v", err)
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()

	valid := Config{Clients: []Client{ClientClaude}, Scope: ScopeProject, MCP: true}
	if err := Validate(valid); err != nil {
		t.Fatalf("a valid config was rejected: %v", err)
	}
	tests := map[string]func(*Config){
		"no client":                  func(c *Config) { c.Clients = nil },
		"unknown client":             func(c *Config) { c.Clients = []Client{"cursor"} },
		"client named twice":         func(c *Config) { c.Clients = []Client{ClientClaude, ClientClaude} },
		"unknown scope":              func(c *Config) { c.Scope = "team" },
		"nothing to set up":          func(c *Config) { c.MCP = false },
		"writes without MCP":         func(c *Config) { c.MCP, c.Skill, c.AllowWrites = false, true, true },
		"Hermes project MCP":         func(c *Config) { c.Clients = []Client{ClientHermes} },
		"Hermes global instructions": func(c *Config) { c.Clients, c.Scope, c.Instructions = []Client{ClientHermes}, ScopeGlobal, true },
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cfg := valid
			change(&cfg)
			if err := Validate(cfg); err == nil {
				t.Fatal("want an error")
			}
		})
	}
}

func TestParseClient(t *testing.T) {
	t.Parallel()

	names := map[string]Client{"codex": ClientCodex, "Claude-Code": ClientClaude, "open-code": ClientOpenCode, "HERMES": ClientHermes}
	for input, want := range names {
		if got, err := ParseClient(input); err != nil || got != want {
			t.Errorf("ParseClient(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	if _, err := ParseClient("cursor"); err == nil {
		t.Error("an unknown client was accepted")
	}
}

// A client launched from the Dock or a launcher does not inherit the shell's
// PATH; Command lets the config name the binary by its full path instead.
func TestCommandPathReachesEveryClient(t *testing.T) {
	t.Parallel()

	project, home := sandbox(t)
	const binary = "/opt/tools/bin/darkubectl"
	cfg := Config{
		Clients: []Client{ClientCodex, ClientClaude, ClientOpenCode, ClientHermes},
		Scope:   ScopeGlobal, MCP: true, Command: binary,
	}
	plan := mustBuild(t, cfg, project, home)
	if len(plan.Changes) != len(cfg.Clients) {
		t.Fatalf("want one config per client, got %s", planPaths(plan, home))
	}
	for _, change := range plan.Changes {
		if !strings.Contains(change.After, binary) {
			t.Errorf("%s does not start %s:\n%s", change.Path, binary, change.After)
		}
	}
}

func TestMCPArgs(t *testing.T) {
	t.Parallel()

	for want, cfg := range map[string]Config{
		"mcp":                 {},
		"mcp --allow-secrets": {AllowSecrets: true},
		// Writes imply secrets on the server, so one flag is enough.
		"mcp --allow-writes": {AllowWrites: true, AllowSecrets: true},
	} {
		if got := strings.Join(mcpArgs(cfg), " "); got != want {
			t.Errorf("mcpArgs(%+v) = %q, want %q", cfg, got, want)
		}
	}
}
