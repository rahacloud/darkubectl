package cmd

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rahacloud/darkubectl/internal/wsexec"
	"github.com/urfave/cli/v3"
)

// `cp` moves files over the exec websocket, which is a terminal and nothing
// else: there is no file channel, no stdin pipe to a command, and anything sent
// is subject to PTY line discipline. So bytes cross as base64 in short lines,
// both ways, and the pod needs `base64` (and `tar`, for a directory) — busybox
// has both, which covers alpine and most slim images.
//
// Downloads read the command's output back out of the terminal stream between
// two markers. Like the exit marker in exec.go, the begin marker is assembled
// by printf from its arguments, so the shell's echo of the command line cannot
// be mistaken for it.

const (
	// cpOperands is SRC and DEST.
	cpOperands = 2

	// Modes for what a copy creates locally: the user's own files, readable
	// the way anything else they make is.
	cpFileMode fs.FileMode = 0o644
	cpDirMode  fs.FileMode = 0o755
)

var (
	errCpArgs = errors.New("usage: darkubectl cp LOCAL APP:REMOTE, or darkubectl cp APP:REMOTE LOCAL")
	errCpBoth = errors.New("exactly one side of cp must be APP:PATH")
)

// remoteRef is the APP:PATH side of a copy.
type remoteRef struct {
	app  string
	path string
}

func newCpCommand() *cli.Command {
	return &cli.Command{
		Name:      "cp",
		Usage:     "Copy files and directories to and from an app's pod",
		ArgsUsage: "SRC DEST",
		Description: "  darkubectl cp ./nginx.conf my-web:/etc/nginx/conf.d/default.conf\n" +
			"  darkubectl cp ./static my-web:/usr/share/nginx/html     # directory: its contents\n" +
			"  darkubectl cp my-db:/var/lib/app/dump.sql ./dump.sql\n" +
			"  darkubectl cp my-web:/etc/nginx ./nginx-conf              # directory, recursively\n\n" +
			"Exactly one side is APP:PATH, like `kubectl cp`. Copying a directory copies its\n" +
			"contents into DEST, which is created if missing. Copying a file onto an existing\n" +
			"directory puts it inside, under its own name.\n\n" +
			"The only channel is the exec terminal, so the pod must be Ready and must have\n" +
			"`base64` (and `tar` for directories). Data crosses base64-encoded in short lines,\n" +
			"which is slow — fine for configs and dumps, not for gigabytes.\n\n" +
			"Files written into the container's filesystem are lost when the pod is replaced;\n" +
			"copy onto a disk mount if they must survive a restart.",
		Flags:  podFlags(),
		Action: cpAction,
	}
}

func cpAction(ctx context.Context, cmd *cli.Command) error {
	if cmd.NArg() != cpOperands {
		return errCpArgs
	}
	src, dst := cmd.Args().Get(0), cmd.Args().Get(1)
	srcRemote, srcIsRemote := parseRemoteRef(src)
	dstRemote, dstIsRemote := parseRemoteRef(dst)
	if srcIsRemote == dstIsRemote {
		return errCpBoth
	}

	app := dstRemote.app
	if srcIsRemote {
		app = srcRemote.app
	}
	t, err := dialExec(ctx, cmd, app)
	if err != nil {
		return err
	}
	defer func() { _ = t.sess.Close() }()

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()

	if srcIsRemote {
		return copyFromPod(ctx, t.sess, srcRemote.path, dst)
	}
	return copyToPod(ctx, t.sess, src, dstRemote.path)
}

// localPathLike matches strings that look like local paths even though they
// contain a colon: a Windows drive letter.
var localPathLike = regexp.MustCompile(`^[A-Za-z]:[\\/]`)

// parseRemoteRef splits APP:PATH. A string is local when it has no colon, when
// it starts like a path (./, ../, /), or when it is a Windows drive path.
func parseRemoteRef(s string) (remoteRef, bool) {
	if strings.HasPrefix(s, "/") || strings.HasPrefix(s, ".") || localPathLike.MatchString(s) {
		return remoteRef{}, false
	}
	app, p, ok := strings.Cut(s, ":")
	if !ok || app == "" || p == "" {
		return remoteRef{}, false
	}
	return remoteRef{app: app, path: p}, true
}

// copyToPod uploads a local file or directory.
func copyToPod(ctx context.Context, sess *wsexec.Session, local, remote string) error {
	info, err := os.Stat(local)
	if err != nil {
		return err
	}

	if !info.IsDir() {
		// Onto an existing directory, the file goes inside it — as cp does.
		isDir, err := remoteIsDir(ctx, sess, remote)
		if err != nil {
			return err
		}
		if isDir {
			remote = path.Join(remote, filepath.Base(local))
		}
		if err := uploadFile(ctx, sess, local, remote); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "copied %s -> %s (%d bytes)\n", local, remote, info.Size())
		return nil
	}

	archive, err := tarDirectory(local)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp("", "darkubectl-cp-*.tar")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(archive); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	remoteTar := path.Join("/tmp", ".darkubectl-cp-"+filepath.Base(tmp.Name()))
	if err := uploadFile(ctx, sess, tmp.Name(), remoteTar); err != nil {
		return err
	}
	extract := fmt.Sprintf("mkdir -p %s && tar xf %s -C %s; s=$?; rm -f %s; [ $s -eq 0 ]",
		shellQuote(remote), shellQuote(remoteTar), shellQuote(remote), shellQuote(remoteTar))
	code, err := runRemoteTo(ctx, sess, extract, discard)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("extracting into %s failed in the pod (is `tar` installed?)", remote)
	}
	fmt.Fprintf(os.Stderr, "copied %s/ -> %s (%d bytes archived)\n", local, remote, len(archive))
	return nil
}

// copyFromPod downloads a remote file or directory.
func copyFromPod(ctx context.Context, sess *wsexec.Session, remote, local string) error {
	isDir, err := remoteIsDir(ctx, sess, remote)
	if err != nil {
		return err
	}

	if isDir {
		data, err := captureRemote(ctx, sess, fmt.Sprintf("tar cf - -C %s . | base64", shellQuote(remote)))
		if err != nil {
			return fmt.Errorf("reading directory %s from the pod (is `tar` installed?): %w", remote, err)
		}
		if err := untarInto(data, local); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "copied %s -> %s/ (%d bytes archived)\n", remote, local, len(data))
		return nil
	}

	data, err := captureRemote(ctx, sess, "base64 < "+shellQuote(remote))
	if err != nil {
		return fmt.Errorf("reading %s from the pod: %w", remote, err)
	}
	if info, statErr := os.Stat(local); statErr == nil && info.IsDir() {
		local = filepath.Join(local, path.Base(remote))
	}
	if err := os.WriteFile(local, data, cpFileMode); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "copied %s -> %s (%d bytes)\n", remote, local, len(data))
	return nil
}

// remoteIsDir reports whether a path in the pod is a directory.
func remoteIsDir(ctx context.Context, sess *wsexec.Session, p string) (bool, error) {
	code, err := runRemoteTo(ctx, sess, "[ -d "+shellQuote(p)+" ]", discard)
	if err != nil {
		return false, err
	}
	return code == 0, nil
}

// captureRemote runs a command whose output is base64 and returns it decoded.
// A non-zero exit is an error: the output of a failed read is not the file.
func captureRemote(ctx context.Context, sess *wsexec.Session, command string) ([]byte, error) {
	nonce, err := newNonce()
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	code, err := runRemoteTo(ctx, sess, beginMarkerLine(nonce)+"; "+command, func(p []byte) { out.Write(p) })
	if err != nil {
		return nil, err
	}
	payload, ok := afterBeginMarker(out.Bytes(), nonce)
	if !ok {
		return nil, errors.New("the pod's output did not contain the start marker")
	}
	if code != 0 {
		return nil, fmt.Errorf("the command exited %d: %s", code, strings.TrimSpace(string(payload)))
	}
	return decodeTerminalBase64(payload)
}

// beginMarkerLine prints the start-of-output marker. Built by printf from its
// arguments for the same reason as exitMarkerLine: so the echoed command line
// never contains it.
func beginMarkerLine(nonce string) string {
	return fmt.Sprintf(`printf '__DK_BEGIN_%%s__\n' '%s'`, nonce)
}

// afterBeginMarker returns what follows the begin marker's line.
func afterBeginMarker(out []byte, nonce string) ([]byte, bool) {
	_, rest, found := bytes.Cut(out, []byte("__DK_BEGIN_"+nonce+"__"))
	if !found {
		return nil, false
	}
	if nl := bytes.IndexByte(rest, '\n'); nl >= 0 {
		rest = rest[nl+1:]
	}
	return rest, true
}

// decodeTerminalBase64 decodes base64 that crossed a PTY, where lines are
// wrapped and end in CRLF.
func decodeTerminalBase64(p []byte) ([]byte, error) {
	clean := bytes.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, p)
	out := make([]byte, base64.StdEncoding.DecodedLen(len(clean)))
	n, err := base64.StdEncoding.Decode(out, clean)
	if err != nil {
		return nil, fmt.Errorf("decode the pod's output: %w", err)
	}
	return out[:n], nil
}

func discard([]byte) {}

// tarDirectory archives a directory's contents, paths relative to it.
func tarDirectory(root string) ([]byte, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil || rel == "." {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() && !info.IsDir() {
			return nil // sockets, devices and symlinks are not copied
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if info.IsDir() {
			hdr.Name += "/"
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		f, err := os.Open(p) //nolint:gosec // walking the user's own directory
		if err != nil {
			return err
		}
		_, err = io.Copy(tw, f)
		_ = f.Close()
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// errTarEscape rejects an archive entry that would land outside the target.
var errTarEscape = errors.New("archive entry escapes the destination directory")

// untarInto extracts an archive into dir, creating it. Entries that would
// escape dir are refused rather than trusted: the archive comes from a pod.
func untarInto(data []byte, dir string) error {
	if err := os.MkdirAll(dir, cpDirMode); err != nil {
		return err
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	tr := tar.NewReader(bytes.NewReader(data))
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read archive from the pod: %w", err)
		}
		target := filepath.Join(root, filepath.FromSlash(hdr.Name))
		if target != root && !strings.HasPrefix(target, root+string(os.PathSeparator)) {
			return fmt.Errorf("%w: %s", errTarEscape, hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, cpDirMode); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := writeTarFile(tr, target, hdr.FileInfo().Mode().Perm()); err != nil {
				return err
			}
		default:
			// Symlinks, devices and the like are skipped: recreating a link
			// from a pod on this machine is exactly the escape refused above.
		}
	}
}

func writeTarFile(r io.Reader, target string, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), cpDirMode); err != nil {
		return err
	}
	f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode) //nolint:gosec // inside the destination
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
