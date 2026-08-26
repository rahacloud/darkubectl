package cmd

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"

	"github.com/coder/websocket"
	"github.com/rahacloud/darkubectl/internal/wsexec"
	"github.com/urfave/cli/v3"
)

// errSessionEndedEarly reports that the shell closed before the command's
// status came back. The command may well have succeeded; the point is that
// nothing observed it, so no caller may treat it as success.
var errSessionEndedEarly = errors.New("remote session ended before the command reported a status")

const (
	flagUpload = "upload"

	// The remote end is an interactive shell on a PTY, so input is subject to
	// terminal line discipline: one very long line is silently truncated, and
	// what survives is handed to the shell as if the user had meant it. Every
	// line this file sends is therefore kept short, and payloads are split
	// rather than written in one frame. 76 is the classic base64 wrap width and
	// leaves generous headroom under any line limit.
	uploadLineLen = 76

	// Bytes of output held back while scanning for the completion marker, so a
	// marker split across two websocket frames is still matched. The marker's
	// own length plus a margin for the status digits.
	markerSlack = 8

	// Nonce entropy. 48 bits is far more than enough to distinguish one
	// command's marker from the next within a single session.
	nonceBytes = 6
)

func newExecCommand() *cli.Command {
	return &cli.Command{
		Name:  "exec",
		Usage: "Run a command in an app's pod",
		Commands: []*cli.Command{
			{
				Name:      cmdApp,
				Aliases:   []string{aliasApp},
				Usage:     "Run a command in an app's pod (over the exec websocket)",
				ArgsUsage: "NAME|ID -- COMMAND [ARGS...]",
				Description: `Runs one command and exits with its status.

  darkubectl exec app my-api -n acme -- sh -c 'ls -l /etc'

--upload copies a local file into the pod before the command runs. The exec
websocket is a terminal, not a file channel, so the file is base64'd and sent as
short lines; the remote side strips CR (the PTY translates line endings) and
decodes to a temporary path, which is moved into place only once the decode
succeeds. A half-written file is never left behind:

  darkubectl exec app prom -n acme --upload ./prometheus.yml:/etc/prometheus/prometheus.yml -- \
      sh -c 'wc -c /etc/prometheus/prometheus.yml'`,
				Flags: append(podFlags(), &cli.StringFlag{
					Name:  flagUpload,
					Usage: "copy a file into the pod first, as LOCAL:REMOTE",
				}),
				Action: execAppAction,
			},
		},
	}
}

func execAppAction(ctx context.Context, cmd *cli.Command) error {
	args := cmd.Args().Slice()
	if len(args) == 0 {
		return errMissingAppRef
	}
	name, command := args[0], args[1:]

	upload := cmd.String(flagUpload)
	if len(command) == 0 && upload == "" {
		return errNoCommand
	}

	var local, remote string
	if upload != "" {
		var ok bool
		local, remote, ok = strings.Cut(upload, ":")
		if !ok || local == "" || remote == "" {
			return fmt.Errorf("--%s takes LOCAL:REMOTE, got %q", flagUpload, upload)
		}
		if _, err := os.Stat(local); err != nil {
			return fmt.Errorf("--%s: %w", flagUpload, err)
		}
	}

	t, err := dialExec(ctx, cmd, name)
	if err != nil {
		return err
	}
	sess := t.sess
	defer func() { _ = sess.Close() }()

	fmt.Fprintf(os.Stderr, "exec in %s (pod %s, container %s)\n", t.appName, t.pod, t.container)

	sigCtx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()

	if upload != "" {
		if err := uploadFile(sigCtx, sess, local, remote); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "uploaded %s -> %s\n", local, remote)
	}
	if len(command) == 0 {
		return nil
	}

	code, err := runRemote(sigCtx, sess, joinCommand(command))
	if err != nil {
		// The shell going away before the marker is not a command failure and
		// has no status to report, but it must not read as success either.
		if errors.Is(err, errSessionEndedEarly) {
			fmt.Fprintln(os.Stderr, "darkubectl: "+err.Error())
			return nil
		}
		return err
	}
	if code != 0 {
		return cli.Exit("", code)
	}
	return nil
}

// uploadFile writes a local file into the pod over the terminal session.
//
// A terminal is the only channel available, so the bytes go across base64'd
// inside a quoted heredoc — quoted so the shell expands nothing in the payload.
// `tr -d '\r'` on the remote side undoes any line-ending translation the PTY
// applies, which would otherwise leave carriage returns in the decoded file.
//
// The decode writes to a sibling temporary path, and the move into place is
// gated on the decoded size matching the local file exactly. That gate is not
// ceremony: if anything truncates the stream — a dropped frame, a heredoc
// terminator that failed to match — `base64 -d` writes a short file and exits
// non-zero, but its status is long gone by the time the move runs. Comparing
// sizes is what turns a silent half-write into a failed upload, and it leaves
// whatever was already at the destination untouched.
func uploadFile(ctx context.Context, sess *wsexec.Session, local, remote string) error {
	data, err := os.ReadFile(local) //nolint:gosec // the path is the user's own argument
	if err != nil {
		return fmt.Errorf("read %s: %w", local, err)
	}
	encoded := base64.StdEncoding.EncodeToString(data)

	tmp := shellQuote(remote + ".dkupload")
	heredoc := "DARKUBECTL_EOF"

	lines := []string{fmt.Sprintf("tr -d '\\r' <<'%s' | base64 -d > %s", heredoc, tmp)}
	for i := 0; i < len(encoded); i += uploadLineLen {
		end := min(i+uploadLineLen, len(encoded))
		lines = append(lines, encoded[i:end])
	}
	lines = append(lines, heredoc)

	for _, line := range lines {
		if err := sess.SendInput(ctx, []byte(line+"\n")); err != nil {
			return err
		}
	}

	verify := fmt.Sprintf(
		`if [ "$(wc -c < %s)" -eq %d ]; then mv %s %s; else rm -f %s; false; fi`,
		tmp, len(data), tmp, shellQuote(remote), tmp)
	code, err := runRemote(ctx, sess, verify)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("upload to %s failed: decoded file did not match %d bytes; destination left unchanged", remote, len(data))
	}
	return nil
}

// runRemote sends one command and returns its exit status.
//
// The exec websocket carries a shell, not a command runner: nothing in the
// stream says "this command finished". So the command is followed by a printf
// of a nonce and `$?`, and the read loop stops at the first match. The nonce and
// the status are printf *arguments* rather than literal text, so the shell's own
// echo of the command line cannot match the pattern the loop is looking for —
// which it would if the marker were written out in full.
func runRemote(ctx context.Context, sess *wsexec.Session, command string) (int, error) {
	nonce, err := newNonce()
	if err != nil {
		return 0, err
	}
	marker := exitMarkerPattern(nonce)

	if err := sess.SendInput(ctx, []byte(exitMarkerLine(command, nonce)+"\n")); err != nil {
		return 0, err
	}

	var pending []byte
	for {
		data, rerr := sess.Read(ctx)
		if rerr != nil {
			// The shell closed (or the user interrupted) before the marker
			// arrived: flush what we have rather than swallowing it.
			emit(pending)
			if isSessionEnd(rerr) {
				return 0, errSessionEndedEarly
			}
			return 0, fmt.Errorf("read exec stream: %w", rerr)
		}
		pending = append(pending, data...)

		if m := marker.FindSubmatchIndex(pending); m != nil {
			emit(pending[:m[0]])
			code, cerr := strconv.Atoi(string(pending[m[2]:m[3]]))
			if cerr != nil {
				return 0, fmt.Errorf("parse remote exit status: %w", cerr)
			}
			return code, nil
		}
		// Hold back only as much as a marker could still be split across.
		if keep := len(marker.String()) + markerSlack; len(pending) > keep {
			emit(pending[:len(pending)-keep])
			pending = pending[len(pending)-keep:]
		}
	}
}

// exitMarkerPattern matches the line the remote shell prints once the command
// has finished, capturing its exit status.
func exitMarkerPattern(nonce string) *regexp.Regexp {
	return regexp.MustCompile(`__DK_EXIT_` + regexp.QuoteMeta(nonce) + `_(\d+)__`)
}

// exitMarkerLine appends the completion marker to a command.
//
// The nonce and the status are printf *arguments*, never literal text in the
// line. That is the whole trick: the shell echoes back everything it is sent,
// so a marker spelled out in full would appear in the output stream before the
// command had run, and the read loop would stop at the echo and report the
// wrong status. Assembled only by printf, the pattern cannot appear until the
// command actually finishes. exitMarkerLine and exitMarkerPattern are a pair
// and only make sense changed together.
func exitMarkerLine(command, nonce string) string {
	return fmt.Sprintf(`%s; printf '\n__DK_EXIT_%%s_%%s__\n' '%s' "$?"`, command, nonce)
}

// emit relays remote output to stdout. A write failure here means stdout itself
// is gone, which nothing in this command can act on and which the shell reports
// on its own, so it is deliberately dropped rather than propagated.
func emit(p []byte) {
	_, _ = os.Stdout.Write(p)
}

// joinCommand rebuilds an argv as a line for the remote shell.
//
// The remote end is a shell, so the argv has to survive being re-parsed by one.
// Joining on spaces does not: `-- sh -c 'wc -c /etc/passwd'` arrives as
// `sh -c wc -c /etc/passwd`, where the shell takes `wc` as the whole -c script
// and the rest as its positional arguments — so it reads stdin and prints
// nothing, having looked like it ran. Quoting every argument keeps the argv the
// caller wrote.
func joinCommand(argv []string) string {
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	return strings.Join(quoted, " ")
}

func isSessionEnd(err error) bool {
	if err == nil {
		return false
	}
	if strings.Contains(err.Error(), wsexec.ErrExited.Error()) {
		return true
	}
	return websocket.CloseStatus(err) != -1
}

func newNonce() (string, error) {
	b := make([]byte, nonceBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// shellQuote wraps a path for /bin/sh single-quoting.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
