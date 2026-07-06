package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/rahacloud/darkubectl/internal/client"
)

// version is overridable at build time via -ldflags "-X ...cmd.version=...".
var version = "dev"

// Shared command-level errors.
var (
	errMissingAppRef = errors.New("an app NAME or ID argument is required")
	errAborted       = errors.New("aborted")
)

// confirm prompts for a yes/no answer on stderr, reading a line from stdin.
// Returns false on EOF or anything other than y/yes.
func confirm(prompt string) bool {
	fmt.Fprintf(os.Stderr, "%s [y/N]: ", prompt)
	line, ok := readLine()
	if !ok {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

// confirmExact requires the user to type an exact string (e.g. a resource name).
func confirmExact(prompt, want string) bool {
	fmt.Fprint(os.Stderr, prompt)
	line, ok := readLine()
	if !ok {
		return false
	}
	return strings.TrimSpace(line) == want
}

func readLine() (string, bool) {
	r := bufio.NewReader(os.Stdin)
	line, err := r.ReadString('\n')
	if err != nil && line == "" {
		return "", false
	}
	return line, true
}

// stateLabel renders an app's live state compactly.
func stateLabel(s client.State) string {
	switch {
	case s.Text != "":
		return s.Text
	case s.StateType != "":
		return s.StateType
	default:
		return "-"
	}
}

func yesNo(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}
