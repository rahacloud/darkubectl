package agentsetup

import (
	"strings"
	"testing"
)

var mcpOnly = []string{"mcp"}

func TestMergeCodexTOML(t *testing.T) {
	t.Parallel()

	const table = "[mcp_servers.darkubectl]\ncommand = \"darkubectl\"\nargs = [\"mcp\"]\n"
	tests := []struct {
		name   string
		before string
		args   []string
		want   string
	}{
		{name: "new file", before: "", args: mcpOnly, want: table},
		{name: "appended after other settings", before: "model = \"x\"\n", args: mcpOnly, want: "model = \"x\"\n\n" + table},
		{
			name:   "arguments are quoted",
			before: "",
			args:   []string{"mcp", "--allow-writes"},
			want:   "[mcp_servers.darkubectl]\ncommand = \"darkubectl\"\nargs = [\"mcp\", \"--allow-writes\"]\n",
		},
		{
			name:   "existing table replaced in place",
			before: "model = \"x\"\n[mcp_servers.darkubectl]\ncommand = \"old\"\n\n[other] # trailing comment\ny = 1\n",
			args:   mcpOnly,
			want:   "model = \"x\"\n" + table + "\n[other] # trailing comment\ny = 1\n",
		},
		{
			// An env table inline, a timeout and a comment are the user's.
			name: "other keys of the table are kept",
			before: "[mcp_servers.darkubectl]\n# pinned to acme\nargs = [\"old\"]\nenv = { DARKUBE_ORG = \"acme\" }\n" +
				"startup_timeout_sec = 20\n",
			args: mcpOnly,
			want: "[mcp_servers.darkubectl]\ncommand = \"darkubectl\"\n# pinned to acme\nargs = [\"mcp\"]\n" +
				"env = { DARKUBE_ORG = \"acme\" }\nstartup_timeout_sec = 20\n",
		},
		{
			name:   "a multi-line value forces a whole rewrite",
			before: "[mcp_servers.darkubectl]\ncommand = \"old\"\nargs = [\n  \"mcp\",\n]\n",
			args:   mcpOnly,
			want:   table,
		},
		{
			// The header is the user's line too, and stays as they wrote it.
			name:   "quoted table name",
			before: "[mcp_servers.\"darkubectl\"]\ncommand = \"old\"\n",
			args:   mcpOnly,
			want:   "[mcp_servers.\"darkubectl\"]\ncommand = \"darkubectl\"\nargs = [\"mcp\"]\n",
		},
		{
			name:   "inline entry becomes a table",
			before: "[mcp_servers]\ndarkubectl = { command = \"old\" }\nother = { command = \"keep\" }\n",
			args:   mcpOnly,
			want:   "[mcp_servers]\nother = { command = \"keep\" }\n\n" + table,
		},
		{
			name:   "a sub-table is not the server table",
			before: "[mcp_servers.darkubectl]\ncommand = \"old\"\n[mcp_servers.darkubectl.env]\nA = \"1\"\n",
			args:   mcpOnly,
			want:   table + "[mcp_servers.darkubectl.env]\nA = \"1\"\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := mergeCodexTOML(tt.before, serverName, tt.args)
			if err != nil {
				t.Fatalf("mergeCodexTOML: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, tt.want)
			}
			// A second run must find nothing to do, or `agent setup` reports a
			// change for ever and the file grows on each run.
			again, err := mergeCodexTOML(got, serverName, tt.args)
			if err != nil {
				t.Fatalf("second mergeCodexTOML: %v", err)
			}
			if again != got {
				t.Fatalf("not idempotent, second run gave:\n%q\nafter:\n%q", again, got)
			}
		})
	}
}

func TestMergeCodexTOMLRejectsDuplicates(t *testing.T) {
	t.Parallel()

	for name, before := range map[string]string{
		"two tables":       "[mcp_servers.darkubectl]\n[mcp_servers.darkubectl]\n",
		"two inline":       "[mcp_servers]\ndarkubectl = {}\ndarkubectl = {}\n",
		"table and inline": "[mcp_servers]\ndarkubectl = {}\n[mcp_servers.darkubectl]\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got, err := mergeCodexTOML(before, serverName, mcpOnly); err == nil {
				t.Fatalf("want an error, got:\n%s", got)
			}
		})
	}
}

type jsonMergeCase struct {
	name   string
	before string
	parent []string
	entry  map[string]any
	want   string
}

//nolint:funlen // a table of whole config files, one per case
func jsonMergeCases() []jsonMergeCase {
	claude := map[string]any{keyCommand: serverName, keyArgs: mcpOnly}
	claudePath := []string{"mcpServers"}
	return []jsonMergeCase{
		{
			name:   "new file",
			parent: claudePath,
			entry:  claude,
			want: `{
  "mcpServers": {
    "darkubectl": {
      "args": [
        "mcp"
      ],
      "command": "darkubectl"
    }
  }
}
`,
		},
		{
			name: "comments, key order and escapes survive",
			before: `{
  // my note
  "z": "a<b", /* inline */
  "a": 1,
}
`,
			parent: []string{"mcp", "servers"},
			entry:  map[string]any{"type": "local", keyCommand: []string{serverName, "mcp"}},
			want: `{
  // my note
  "z": "a<b", /* inline */
  "a": 1,
  "mcp": {
    "servers": {
      "darkubectl": {
        "command": [
          "darkubectl",
          "mcp"
        ],
        "type": "local"
      }
    }
  }
}
`,
		},
		{
			name: "added beside other servers",
			before: `{
	"mcpServers": {
		"other": {"command": "x"} // keep
	},
	"last": true
}
`,
			parent: claudePath,
			entry:  claude,
			want: `{
	"mcpServers": {
		"other": {"command": "x"}, // keep
		"darkubectl": {
			"args": [
				"mcp"
			],
			"command": "darkubectl"
		}
	},
	"last": true
}
`,
		},
		{
			name: "existing entry keeps its other keys",
			before: `{
  "mcpServers": {
    "darkubectl": {
      "command": "/old/darkubectl",
      "env": {"DARKUBE_ORG": "acme"}
    }
  }
}
`,
			parent: claudePath,
			entry:  claude,
			want: `{
  "mcpServers": {
    "darkubectl": {
      "command": "darkubectl",
      "env": {"DARKUBE_ORG": "acme"},
      "args": [
        "mcp"
      ]
    }
  }
}
`,
		},
		{
			name:   "an equal entry in the user's own formatting is left alone",
			before: `{"mcpServers": {"darkubectl": {"command": "darkubectl", "args": ["mcp"]}}}` + "\n",
			parent: claudePath,
			entry:  claude,
			want:   `{"mcpServers": {"darkubectl": {"command": "darkubectl", "args": ["mcp"]}}}` + "\n",
		},
		{
			name:   "single-line object",
			before: `{"a": {"b": "}"}}`,
			parent: claudePath,
			entry:  map[string]any{keyCommand: serverName},
			want: `{"a": {"b": "}"},
  "mcpServers": {
    "darkubectl": {
      "command": "darkubectl"
    }
  }
}`,
		},
		{
			name:   "empty object",
			before: "{}\n",
			parent: claudePath,
			entry:  map[string]any{keyCommand: serverName},
			want: `{
  "mcpServers": {
    "darkubectl": {
      "command": "darkubectl"
    }
  }
}
`,
		},
	}
}

func TestMergeJSONServer(t *testing.T) {
	t.Parallel()

	for _, tt := range jsonMergeCases() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := mergeJSONServer(tt.before, tt.parent, tt.entry)
			if err != nil {
				t.Fatalf("mergeJSONServer: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, tt.want)
			}
			if err := checkJSONObjects(got, nil); err != nil {
				t.Fatalf("result is not valid JSONC: %v", err)
			}
			again, err := mergeJSONServer(got, tt.parent, tt.entry)
			if err != nil {
				t.Fatalf("second mergeJSONServer: %v", err)
			}
			if again != got {
				t.Fatalf("not idempotent, second run gave:\n%s", again)
			}
		})
	}
}

func TestMergeJSONServerRejects(t *testing.T) {
	t.Parallel()

	for name, before := range map[string]string{
		"root is an array":       `[]`,
		"servers is not object":  `{"mcpServers": 3}`,
		"entry is not an object": `{"mcpServers": {"darkubectl": "x"}}`,
		"two documents":          `{} {}`,
		"unterminated comment":   `{} /* never closed`,
		"broken syntax":          `{"mcpServers": `,
		"stray comma":            `{,}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := mergeJSONServer(before, []string{"mcpServers"}, map[string]any{keyCommand: serverName})
			if err == nil {
				t.Fatalf("want an error, got:\n%s", got)
			}
		})
	}
}

func TestMergeYAMLServer(t *testing.T) {
	t.Parallel()

	got, err := mergeYAMLServer("# my hermes config\nmodel: x\n", serverName, mcpOnly)
	if err != nil {
		t.Fatalf("mergeYAMLServer: %v", err)
	}
	for _, want := range []string{"# my hermes config", "model: x", "mcp_servers:", "darkubectl:", "command: darkubectl", "- mcp"} {
		if !strings.Contains(got, want) {
			t.Errorf("result lacks %q:\n%s", want, got)
		}
	}

	// An entry that is already right is returned byte for byte, whatever its
	// formatting, rather than re-encoded.
	const own = "mcp_servers:\n  darkubectl: {command: darkubectl, args: [mcp]}\n"
	same, err := mergeYAMLServer(own, serverName, mcpOnly)
	if err != nil {
		t.Fatalf("mergeYAMLServer: %v", err)
	}
	if same != own {
		t.Errorf("an up-to-date config was rewritten:\n%s", same)
	}

	changed, err := mergeYAMLServer(own, serverName, []string{"mcp", "--allow-writes"})
	if err != nil {
		t.Fatalf("mergeYAMLServer: %v", err)
	}
	if !strings.Contains(changed, "--allow-writes") {
		t.Errorf("new arguments were not written:\n%s", changed)
	}

	if out, err := mergeYAMLServer("mcp_servers: 3\n", serverName, mcpOnly); err == nil {
		t.Errorf("want an error for a scalar mcp_servers, got:\n%s", out)
	}
}

func TestUpsertBlocks(t *testing.T) {
	t.Parallel()

	first, err := upsertManaged("# Project\n\nMine.")
	if err != nil {
		t.Fatalf("upsertManaged: %v", err)
	}
	head := "# Project\n\nMine.\n\n" + instructionStart + "\n## Darkubectl\n"
	if !strings.HasPrefix(first, head) || !strings.HasSuffix(first, instructionEnd+"\n") {
		t.Fatalf("unexpected result:\n%s", first)
	}
	// Refreshing replaces the block and nothing around it.
	stale := strings.Replace(first, "## Darkubectl", "## Old heading", 1) + "\nAfter.\n"
	refreshed, err := upsertManaged(stale)
	if err != nil {
		t.Fatalf("upsertManaged: %v", err)
	}
	if refreshed != first+"\nAfter.\n" {
		t.Fatalf("refresh changed more than the block:\n%s", refreshed)
	}
	if _, err := upsertManaged(instructionEnd + "\n" + instructionStart); err == nil {
		t.Error("markers out of order were accepted")
	}
	if _, err := upsertManaged(instructionStart); err == nil {
		t.Error("a lone start marker was accepted")
	}

	bridge, err := upsertClaudeBridge("")
	if err != nil {
		t.Fatalf("upsertClaudeBridge: %v", err)
	}
	if bridge != claudeBridgeStart+"\n@AGENTS.md\n"+claudeBridgeEnd+"\n" {
		t.Fatalf("unexpected bridge:\n%s", bridge)
	}
}

func TestUpsertSkill(t *testing.T) {
	t.Parallel()

	// The marker is what lets a later run recognise its own file; without it in
	// the embedded text every update would be refused as someone else's skill.
	if !strings.Contains(skillText, skillMarker) {
		t.Fatal("skill.md lacks the managed marker")
	}
	if !strings.HasPrefix(skillText, "---\nname: darkubectl\n") {
		t.Fatal("skill.md does not start with its front matter")
	}
	if got, err := upsertSkill("old text\n" + skillMarker); err != nil || got != skillText {
		t.Fatalf("a managed skill was not updated: %v", err)
	}
	if _, err := upsertSkill("# someone else's skill\n"); err == nil {
		t.Fatal("a foreign skill was overwritten")
	}
}

func TestSimpleDiff(t *testing.T) {
	t.Parallel()

	got := strings.Join(simpleDiff("a\nb\nc\n", "a\nx\ny\nc\n"), "|")
	if got != "- b|+ x|+ y" {
		t.Fatalf("got %q", got)
	}
	if got := simpleDiff("", "a\n"); len(got) != 1 || got[0] != "+ a" {
		t.Fatalf("got %q", got)
	}
}
