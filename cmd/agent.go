package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/rahacloud/darkubectl/internal/agentsetup"
	"github.com/rahacloud/darkubectl/internal/tui"
	"github.com/urfave/cli/v3"
	"golang.org/x/term"
)

const (
	flagAgentClient       = "client"
	flagAgentScope        = "scope"
	flagAgentMCP          = "mcp"
	flagAgentSkill        = "skill"
	flagAgentInstructions = "instructions"
)

func newAgentCommand() *cli.Command {
	return &cli.Command{
		Name:  "agent",
		Usage: "Configure AI agent clients",
		Commands: []*cli.Command{
			{
				Name:  "setup",
				Usage: "Set up darkubectl MCP, skill, or instructions for agent clients",
				Description: "Run the interactive setup wizard, or pass flags to configure clients directly. " +
					"Project instructions use AGENTS.md with a Claude Code import bridge.",
				Flags: []cli.Flag{
					&cli.StringSliceFlag{Name: flagAgentClient, Usage: "agent client: codex, claude, opencode, or hermes (repeatable)"},
					&cli.StringFlag{Name: flagAgentScope, Value: string(agentsetup.ScopeProject), Usage: "setup scope: project or global"},
					&cli.BoolFlag{Name: flagAgentMCP, Usage: "configure the darkubectl MCP server"},
					&cli.BoolFlag{Name: flagAgentSkill, Usage: "install or update the darkubectl skill"},
					&cli.BoolFlag{Name: flagAgentInstructions, Usage: "add or update managed agent instructions"},
					&cli.BoolFlag{Name: flagAllowWrites, Usage: "allow MCP write commands (also enables secret access)"},
					&cli.BoolFlag{Name: flagAllowSecrets, Usage: "allow MCP commands that print secrets"},
					&cli.BoolFlag{Name: flagYes, Aliases: []string{aliasYes}, Usage: "apply changes without the confirmation prompt"},
				},
				Action: agentSetupAction,
			},
		},
	}
}

func agentSetupAction(_ context.Context, cmd *cli.Command) error {
	if cmd.Args().Len() != 0 {
		return errors.New("agent setup does not accept positional arguments")
	}
	flags := []string{
		flagAgentClient, flagAgentScope, flagAgentMCP, flagAgentSkill,
		flagAgentInstructions, flagAllowWrites, flagAllowSecrets,
	}
	direct := false
	for _, flag := range flags {
		direct = direct || cmd.IsSet(flag)
	}
	if !direct {
		if cmd.Bool(flagYes) {
			return errors.New("--yes requires explicit setup flags; use agent setup interactively or provide --client, --scope, and a setup item")
		}
		if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
			return errors.New("agent setup needs a terminal; use --client, --scope, and a setup item for non-interactive use")
		}
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("find home directory: %w", err)
		}
		plan, approved, err := tui.RunAgentSetup(cwd, home)
		if err != nil {
			return fmt.Errorf("agent setup wizard: %w", err)
		}
		if !approved {
			fmt.Fprintln(os.Stdout, "Agent setup cancelled.")
			return nil
		}
		return applyAgentSetup(plan)
	}

	var clients []agentsetup.Client
	for _, value := range cmd.StringSlice(flagAgentClient) {
		client, err := agentsetup.ParseClient(value)
		if err != nil {
			return err
		}
		clients = append(clients, client)
	}
	cfg := agentsetup.Config{
		Clients:      clients,
		Scope:        agentsetup.Scope(cmd.String(flagAgentScope)),
		MCP:          cmd.Bool(flagAgentMCP),
		Skill:        cmd.Bool(flagAgentSkill),
		Instructions: cmd.Bool(flagAgentInstructions),
		AllowWrites:  cmd.Bool(flagAllowWrites),
		AllowSecrets: cmd.Bool(flagAllowSecrets),
	}
	plan, err := buildAgentSetupPlan(cfg)
	if err != nil {
		return err
	}
	if len(plan.Changes) == 0 {
		fmt.Fprintln(os.Stdout, "Agent setup is already up to date.")
		return nil
	}
	fmt.Fprintln(os.Stdout, strings.Join(plan.ReviewLines(), "\n"))
	if !cmd.Bool(flagYes) {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return errors.New("confirmation needs a terminal; pass --yes to apply this reviewed plan")
		}
		if !confirm() {
			fmt.Fprintln(os.Stdout, "Agent setup cancelled.")
			return nil
		}
	}
	return applyAgentSetup(plan)
}

func buildAgentSetupPlan(cfg agentsetup.Config) (*agentsetup.Plan, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get working directory: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("find home directory: %w", err)
	}
	return agentsetup.Build(cfg, cwd, home)
}

func applyAgentSetup(plan *agentsetup.Plan) error {
	if err := plan.Apply(); err != nil {
		return fmt.Errorf("apply agent setup: %w", err)
	}
	for _, change := range plan.Changes {
		fmt.Fprintf(os.Stdout, "Configured %s\n", change.Path)
	}
	return nil
}
