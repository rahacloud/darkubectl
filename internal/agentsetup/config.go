package agentsetup

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	serverName = "darkubectl"
	keyCommand = "command"
	keyArgs    = "args"

	codexServers = "mcp_servers"

	yamlMapTag = "!!map"
	yamlStrTag = "!!str"
	yamlSeqTag = "!!seq"
)

// codexLocation is where a Codex config already defines the darkubectl server:
// as its own table (lines start to end, end exclusive) or as an inline table
// under [mcp_servers]. An index of -1 means absent.
type codexLocation struct {
	start, end, inline int
}

// mergeCodexTOML puts the darkubectl server table into a Codex config.toml. An
// existing table is replaced where it stands, so the rest of the file keeps its
// order and comments, and a second run changes nothing.
func mergeCodexTOML(before string, args []string) (string, error) {
	lines := strings.Split(before, "\n")
	loc, err := locateCodexServer(lines)
	if err != nil {
		return "", err
	}
	block := codexTOML(args)
	if loc.start >= 0 {
		out := strings.Join(slices.Concat(lines[:loc.start], block, lines[loc.end:]), "\n")
		if !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		return out, nil
	}
	if loc.inline >= 0 {
		lines = slices.Delete(slices.Clone(lines), loc.inline, loc.inline+1)
	}
	body := strings.TrimRight(strings.Join(lines, "\n"), "\n")
	if body != "" {
		body += "\n\n"
	}
	return body + strings.Join(block, "\n") + "\n", nil
}

func locateCodexServer(lines []string) (codexLocation, error) {
	loc := codexLocation{start: -1, end: -1, inline: -1}
	insideServers := false
	for i, line := range lines {
		if section, ok := tomlSection(line); ok {
			insideServers = section == codexServers
			switch {
			case section == codexServers+"."+serverName || section == codexServers+`."`+serverName+`"`:
				if loc.start >= 0 {
					return loc, errors.New("config has duplicate darkubectl MCP sections")
				}
				loc.start = i
			case loc.start >= 0 && loc.end < 0:
				loc.end = i
			}
			continue
		}
		if !insideServers {
			continue
		}
		if key, _, ok := strings.Cut(line, "="); ok && strings.TrimSpace(key) == serverName {
			if loc.inline >= 0 {
				return loc, errors.New("config has duplicate darkubectl MCP entries")
			}
			loc.inline = i
		}
	}
	if loc.start >= 0 && loc.inline >= 0 {
		return loc, errors.New("config defines darkubectl twice")
	}
	if loc.start < 0 {
		return loc, nil
	}
	if loc.end < 0 {
		loc.end = len(lines)
	}
	// The blank lines after the table separate it from what follows; they are
	// not part of it, or every run would add one more.
	for loc.end > loc.start+1 && strings.TrimSpace(lines[loc.end-1]) == "" {
		loc.end--
	}
	return loc, nil
}

// tomlSection reports the name of a [table] header line, ignoring a trailing
// comment. Arrays of tables ([[name]]) are not sections for this purpose.
func tomlSection(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	quote := byte(0)
	escaped := false
	for i := 0; i < len(trimmed); i++ {
		ch := trimmed[i]
		switch {
		case quote == '"' && escaped:
			escaped = false
		case quote == '"' && ch == '\\':
			escaped = true
		case quote != 0:
			if ch == quote {
				quote = 0
			}
		case ch == '"' || ch == '\'':
			quote = ch
		case ch == '#':
			trimmed = strings.TrimSpace(trimmed[:i])
		}
	}
	if len(trimmed) < len("[x]") || trimmed[0] != '[' || trimmed[len(trimmed)-1] != ']' || strings.HasPrefix(trimmed, "[[") {
		return "", false
	}
	return strings.TrimSpace(trimmed[1 : len(trimmed)-1]), true
}

func codexTOML(args []string) []string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = strconv.Quote(arg)
	}
	return []string{
		"[" + codexServers + "." + serverName + "]",
		keyCommand + ` = "` + serverName + `"`,
		keyArgs + " = [" + strings.Join(quoted, ", ") + "]",
	}
}

// mergeYAMLServer puts the darkubectl server into Hermes's config.yaml. It
// edits the node tree rather than a decoded map so comments and key order
// survive, and returns the input untouched when the entry is already right.
func mergeYAMLServer(before string, args []string) (string, error) {
	root := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: yamlMapTag}}}
	if strings.TrimSpace(before) != "" {
		if err := yaml.Unmarshal([]byte(before), root); err != nil {
			return "", fmt.Errorf("parse Hermes YAML config: %w", err)
		}
		if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
			return "", errors.New("the Hermes YAML config root must be a mapping")
		}
	}
	servers, err := childMapping(root.Content[0], codexServers)
	if err != nil {
		return "", err
	}
	server, err := childMapping(servers, serverName)
	if err != nil {
		return "", err
	}
	if yamlServerMatches(server, args) {
		return before, nil
	}
	setMappingNode(server, keyCommand, &yaml.Node{Kind: yaml.ScalarNode, Tag: yamlStrTag, Value: serverName})
	sequence := &yaml.Node{Kind: yaml.SequenceNode, Tag: yamlSeqTag}
	for _, arg := range args {
		sequence.Content = append(sequence.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: yamlStrTag, Value: arg})
	}
	setMappingNode(server, keyArgs, sequence)
	encoded, err := yaml.Marshal(root)
	if err != nil {
		return "", fmt.Errorf("encode Hermes YAML config: %w", err)
	}
	return string(encoded), nil
}

// childMapping returns the mapping stored under key, creating it when absent.
func childMapping(parent *yaml.Node, key string) (*yaml.Node, error) {
	child := mappingNodeValue(parent, key)
	if child == nil {
		child = &yaml.Node{Kind: yaml.MappingNode, Tag: yamlMapTag}
		setMappingNode(parent, key, child)
	}
	if child.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("the Hermes config key %s must be a mapping", key)
	}
	return child, nil
}

func yamlServerMatches(server *yaml.Node, args []string) bool {
	command := mappingNodeValue(server, keyCommand)
	sequence := mappingNodeValue(server, keyArgs)
	if command == nil || command.Value != serverName || sequence == nil || sequence.Kind != yaml.SequenceNode {
		return false
	}
	return slices.EqualFunc(sequence.Content, args, func(node *yaml.Node, arg string) bool {
		return node.Kind == yaml.ScalarNode && node.Value == arg
	})
}

func mappingNodeValue(mapping *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

func setMappingNode(mapping *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content[i+1] = value
			return
		}
	}
	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: yamlStrTag, Value: key},
		value,
	)
}
