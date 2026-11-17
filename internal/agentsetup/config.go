package agentsetup

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

func mergeCodexTOML(before string, args []string) (string, error) {
	lines := strings.Split(before, "\n")
	targetStart, targetEnd := -1, -1
	inlineStart := -1
	insideMCP := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if section, ok := tomlSection(line); ok {
			insideMCP = section == "mcp_servers"
			if section == "mcp_servers.darkubectl" || section == "mcp_servers.\"darkubectl\"" {
				if targetStart >= 0 {
					return "", errors.New("config has duplicate darkubectl MCP sections")
				}
				targetStart = i
			} else if targetStart >= 0 && targetEnd < 0 {
				targetEnd = i
			}
			continue
		}
		if insideMCP && strings.HasPrefix(trimmed, "darkubectl") {
			key, _, ok := strings.Cut(trimmed, "=")
			if ok && strings.TrimSpace(key) == "darkubectl" {
				if inlineStart >= 0 {
					return "", errors.New("config has duplicate darkubectl MCP entries")
				}
				inlineStart = i
			}
		}
	}
	if targetStart >= 0 && targetEnd < 0 {
		targetEnd = len(lines)
	}
	if targetStart >= 0 && inlineStart >= 0 {
		return "", errors.New("config defines darkubectl twice")
	}
	generated := codexTOML(args)
	if targetStart >= 0 {
		var out strings.Builder
		for _, line := range lines[:targetStart] {
			out.WriteString(line + "\n")
		}
		out.WriteString(generated)
		if targetEnd < len(lines) {
			for _, line := range lines[targetEnd:] {
				out.WriteString(line + "\n")
			}
		}
		return out.String(), nil
	}
	if inlineStart >= 0 {
		lines = append(lines[:inlineStart], lines[inlineStart+1:]...)
		before = strings.Join(lines, "\n")
	}
	if before != "" && !strings.HasSuffix(before, "\n") {
		before += "\n"
	}
	if before != "" && !strings.HasSuffix(before, "\n\n") {
		before += "\n"
	}
	return before + generated, nil
}

func tomlSection(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	quote := byte(0)
	escaped := false
	for i := 0; i < len(trimmed); i++ {
		ch := trimmed[i]
		if quote != 0 {
			if quote == '"' && escaped {
				escaped = false
				continue
			}
			if quote == '"' && ch == '\\' {
				escaped = true
				continue
			}
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			quote = ch
			continue
		}
		if ch == '#' {
			trimmed = strings.TrimSpace(trimmed[:i])
			break
		}
	}
	if len(trimmed) < 3 || trimmed[0] != '[' || trimmed[len(trimmed)-1] != ']' || strings.HasPrefix(trimmed, "[[") {
		return "", false
	}
	return strings.TrimSpace(trimmed[1 : len(trimmed)-1]), true
}

func codexTOML(args []string) string {
	var b strings.Builder
	b.WriteString("[mcp_servers.darkubectl]\n")
	b.WriteString("command = \"darkubectl\"\n")
	b.WriteString("args = [")
	for i, arg := range args {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(strconv.Quote(arg))
	}
	b.WriteString("]\n\n")
	return b.String()
}

func mergeJSONServer(before string, parent []string, entry map[string]any) (string, error) {
	root := map[string]any{}
	if strings.TrimSpace(before) != "" {
		cleaned, err := stripJSONC(before)
		if err != nil {
			return "", err
		}
		decoder := json.NewDecoder(strings.NewReader(cleaned))
		decoder.UseNumber()
		if err := decoder.Decode(&root); err != nil {
			return "", fmt.Errorf("parse JSON config: %w", err)
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			if err == nil {
				return "", errors.New("parse JSON config: multiple top-level values")
			}
			return "", fmt.Errorf("parse JSON config: %w", err)
		}
	}
	if root == nil {
		return "", errors.New("JSON config root must be an object")
	}
	current := root
	for _, key := range parent[:len(parent)-1] {
		value, ok := current[key]
		if !ok {
			child := map[string]any{}
			current[key] = child
			current = child
			continue
		}
		child, ok := value.(map[string]any)
		if !ok {
			return "", fmt.Errorf("JSON config key %q must be an object", key)
		}
		current = child
	}
	key := parent[len(parent)-1]
	servers, ok := current[key]
	if !ok {
		servers = map[string]any{}
		current[key] = servers
	}
	serverMap, ok := servers.(map[string]any)
	if !ok {
		return "", fmt.Errorf("JSON config key %q must be an object", key)
	}
	old, ok := serverMap["darkubectl"]
	if ok {
		oldMap, ok := old.(map[string]any)
		if !ok {
			return "", errors.New("darkubectl MCP entry must be an object")
		}
		for k, v := range entry {
			oldMap[k] = v
		}
	} else {
		serverMap["darkubectl"] = entry
	}
	encoded, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode JSON config: %w", err)
	}
	return string(encoded) + "\n", nil
}

func stripJSONC(s string) (string, error) {
	var b strings.Builder
	b.Grow(len(s))
	inString, escaped := false, false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inString {
			b.WriteByte(ch)
			if escaped {
				escaped = false
			} else if ch == '\\' {
				escaped = true
			} else if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			b.WriteByte(ch)
			continue
		}
		if ch == '/' && i+1 < len(s) && s[i+1] == '/' {
			for i < len(s) && s[i] != '\n' {
				i++
			}
			if i < len(s) {
				b.WriteByte('\n')
			}
			continue
		}
		if ch == '/' && i+1 < len(s) && s[i+1] == '*' {
			i += 2
			for i < len(s) && !(s[i-1] == '*' && s[i] == '/') {
				if s[i] == '\n' {
					b.WriteByte('\n')
				}
				i++
			}
			if i >= len(s) {
				return "", errors.New("unterminated JSONC comment")
			}
			continue
		}
		b.WriteByte(ch)
	}
	if inString {
		return "", errors.New("unterminated JSON string")
	}
	return stripJSONTrailingCommas(b.String()), nil
}

func stripJSONTrailingCommas(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inString, escaped := false, false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inString {
			b.WriteByte(ch)
			if escaped {
				escaped = false
			} else if ch == '\\' {
				escaped = true
			} else if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			b.WriteByte(ch)
			continue
		}
		if ch == ',' {
			next := i + 1
			for next < len(s) && (s[next] == ' ' || s[next] == '\t' || s[next] == '\n' || s[next] == '\r') {
				next++
			}
			if next < len(s) && (s[next] == '}' || s[next] == ']') {
				continue
			}
		}
		b.WriteByte(ch)
	}
	return b.String()
}

func mergeYAMLServer(before string, entry map[string]any) (string, error) {
	root := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	if strings.TrimSpace(before) != "" {
		if err := yaml.Unmarshal([]byte(before), root); err != nil {
			return "", fmt.Errorf("parse Hermes YAML config: %w", err)
		}
		if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
			return "", errors.New("Hermes YAML config root must be a mapping")
		}
	}
	top := root.Content[0]
	mcpServers := mappingNodeValue(top, "mcp_servers")
	if mcpServers == nil {
		mcpServers = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		setMappingNode(top, "mcp_servers", mcpServers)
	}
	if mcpServers.Kind != yaml.MappingNode {
		return "", errors.New("Hermes config key mcp_servers must be a mapping")
	}
	server := mappingNodeValue(mcpServers, "darkubectl")
	if server == nil {
		server = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		setMappingNode(mcpServers, "darkubectl", server)
	}
	if server.Kind != yaml.MappingNode {
		return "", errors.New("Hermes darkubectl MCP entry must be a mapping")
	}
	setMappingNode(server, "command", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "darkubectl"})
	args, ok := entry["args"].([]any)
	if !ok {
		return "", errors.New("internal setup error: MCP arguments are missing")
	}
	sequence := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, arg := range args {
		value, ok := arg.(string)
		if !ok {
			return "", errors.New("internal setup error: MCP argument is not a string")
		}
		sequence.Content = append(sequence.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
	}
	setMappingNode(server, "args", sequence)
	encoded, err := yaml.Marshal(root)
	if err != nil {
		return "", fmt.Errorf("encode Hermes YAML config: %w", err)
	}
	return string(encoded), nil
}

func mappingNodeValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
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
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		value,
	)
}
