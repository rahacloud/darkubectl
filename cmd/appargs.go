package cmd

import (
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// The container entrypoint fields are the sharpest edge in the whole API, and
// the asymmetry is not documented anywhere upstream. Established by experiment
// against a live app on 2026-08-27:
//
//	command  is SPLIT on whitespace     -> ["/bin/sh", "-c"]
//	args     is NOT split, ever         -> ["<the whole string>"]
//
// So the arrangement everyone reaches for first —
//
//	command: /bin/sh
//	args:    -c echo hello
//
// hands the container argv[1] = "-c echo hello" as a single token. busybox sh
// reads the space after -c as another flag and dies with
//
//	/bin/sh: illegal option -
//
// in a crash loop, with nothing in the API response pointing at the cause. The
// working form puts the flag in command, where splitting happens:
//
//	command: /bin/sh -c
//	args:    echo hello
//
// and because args is never split, the script arrives as the one argument `-c`
// wants, spaces and all. An earlier reading of this held that the script had
// to avoid whitespace ($IFS in place of spaces); that was wrong. Corrected
// 2026-09-27 against mssql-log-backup in talaland-production, which runs
// `command: /bin/sh -c` with a 48-word script in args, healthy with no
// restarts. Whitespace in args is only a mistake when nothing is waiting for a
// single argument — which is what the warning below still catches.
//
// None of that is guessable, so the checks below exist to say it out loud at
// the one moment the user can act on it.

// errArgsMultiList is returned when args is given as a multi-element YAML list.
// A list implies "these are separate arguments", which the API cannot express:
// args is delivered as exactly one argv entry.
var errArgsMultiList = errors.New(
	"args was given as a list of several items, but Darkube passes args to the container as a " +
		"single argument and never splits it. Put the words that need splitting in `command` " +
		"(which is split on whitespace), or join them yourself if they really are one argument")

// shellWords is a command/args field that accepts either a plain YAML string or
// a YAML list, so a spec can be written either way:
//
//	command: /bin/sh -c
//	command: ["/bin/sh", "-c"]
//
// The API takes a string in both cases; a list is joined with single spaces,
// which is exactly what command's own splitting will undo.
type shellWords struct {
	Words []string
	// IsList records that the YAML gave a sequence rather than a scalar. It is
	// what lets args reject a multi-element list while still accepting a
	// one-element one.
	IsList bool
}

// UnmarshalYAML accepts a scalar or a sequence of scalars.
func (s *shellWords) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		var str string
		if err := node.Decode(&str); err != nil {
			return err
		}
		s.Words, s.IsList = []string{str}, false
		return nil
	case yaml.SequenceNode:
		var list []string
		if err := node.Decode(&list); err != nil {
			return err
		}
		s.Words, s.IsList = list, true
		return nil
	default:
		return fmt.Errorf("expected a string or a list of strings, got %v", node.Kind)
	}
}

// MarshalYAML writes the value back the way it was given: a list stays a list.
func (s shellWords) MarshalYAML() (any, error) {
	if s.IsList {
		return s.Words, nil
	}
	return s.String(), nil
}

// IsZero reports an absent value, so `omitempty` leaves it out of a spec.
// An explicitly empty string is not zero: it means "clear the field".
func (s shellWords) IsZero() bool { return s.Words == nil }

// String renders the value as the single string the API stores.
func (s shellWords) String() string { return strings.Join(s.Words, " ") }

// newShellWords wraps a plain string (from a flag or a prompt).
func newShellWords(s string) shellWords {
	if s == "" {
		return shellWords{}
	}
	return shellWords{Words: []string{s}}
}

// validateArgs rejects the one shape that cannot work.
func validateArgs(args shellWords) error {
	if args.IsList && len(args.Words) > 1 {
		return errArgsMultiList
	}
	return nil
}

// entrypointWarnings returns human-readable warnings about a command/args pair.
// These are warnings rather than errors: a single argument containing a space is
// legal, just very rarely what someone means.
func entrypointWarnings(command, args string) []string {
	var out []string

	// `sh -c` wants its script as exactly one argument, so whitespace in args
	// is right there. Anywhere else it almost always means words that were
	// meant to be split.
	if strings.ContainsFunc(args, isSpace) && !runsScript(command) {
		out = append(out,
			"args contains whitespace. Darkube passes args to the container as ONE argument and does "+
				"not split it, so the container will receive the whole string as a single value — "+
				"which is almost never intended, and typically crash-loops with an error like "+
				"`/bin/sh: illegal option -`.\n"+
				"    Words that need splitting belong in `command`, which IS split on whitespace. To run\n"+
				"    a script, put the shell and its -c there and the whole script in args:\n"+
				"        command: \"/bin/sh -c\"\n"+
				"        args:    \"<the script, spaces and all>\"")
	}

	// A shell invoked with no -c will read from stdin, find none, and exit 0
	// immediately, which reads as a container that "started fine" and vanished.
	if isShell(command) && !strings.Contains(command, "-c") && args != "" {
		out = append(out,
			"command is a shell but carries no -c, so the shell will not execute args as a script. "+
				"Use `command: \""+command+" -c\"` and put the script in args.")
	}

	return out
}

// runsScript reports whether command is a shell told to run its next argument
// as a script: `sh -c`, `/bin/bash -c` and the like, with -c last.
func runsScript(command string) bool {
	fields := strings.Fields(command)
	return len(fields) >= 2 && isShell(command) && fields[len(fields)-1] == "-c"
}

func isSpace(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' }

// isShell reports whether command names a POSIX-ish shell, ignoring any path.
func isShell(command string) bool {
	first, _, _ := strings.Cut(strings.TrimSpace(command), " ")
	if idx := strings.LastIndex(first, "/"); idx >= 0 {
		first = first[idx+1:]
	}
	switch first {
	case "sh", "ash", "bash", "dash", "zsh", "busybox":
		return true
	default:
		return false
	}
}
