package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/urfave/cli/v3"
)

const (
	flagData   = "data"
	flagHeader = "header"
	// apiArgs is METHOD and PATH.
	apiArgs = 2
)

var (
	errAPIUsage  = errors.New("usage: darkubectl api METHOD PATH (for example: api GET /api/v1/darkube/plans/)")
	errAPIMethod = errors.New("method must be one of GET, POST, PUT, PATCH, DELETE, OPTIONS")
	errAPIHeader = errors.New(`header must be "Name: value"`)
)

// newAPICommand is darkubectl's `kubectl get --raw`: one authenticated request
// to any path, with the body printed as returned. The Darkube API is
// reverse-engineered, and calling a route is how its shape gets learned.
func newAPICommand() *cli.Command {
	return &cli.Command{
		Name:      "api",
		Usage:     "Send one authenticated request to any API path and print the response",
		ArgsUsage: "METHOD PATH",
		Description: "  darkubectl api GET /api/v1/darkube/plans/\n" +
			"  darkubectl api OPTIONS /api/v1/darkube/apps/<uuid>/\n" +
			"  darkubectl api POST /api/v1/darkube/apps/<uuid>/restart/ -d '{}'\n\n" +
			"Uses the same credential and X-Organization as every other command. A query\n" +
			"string may be part of PATH. -d takes a JSON body, or @file, or @- for stdin.\n\n" +
			"-H adds a request header and may be repeated. The one that matters in practice is\n" +
			"the TOTP code some writes demand, such as creating an object-storage key:\n\n" +
			"  darkubectl api POST /storage/v2/key/ -H 'x-otp: 123456' -d @key.json\n\n" +
			"Nothing is confirmed first: a PUT or DELETE here is sent as written.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagData, Aliases: []string{"d"}, Usage: "JSON request body, @file, or @- for stdin"},
			&cli.StringSliceFlag{Name: flagHeader, Aliases: []string{"H"}, Usage: "extra request header as 'Name: value' (repeatable)"},
		},
		Action: apiAction,
	}
}

func apiAction(ctx context.Context, cmd *cli.Command) error {
	if cmd.Args().Len() != apiArgs {
		return errAPIUsage
	}
	method := strings.ToUpper(cmd.Args().Get(0))
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions:
	default:
		return errAPIMethod
	}

	u, err := url.Parse(cmd.Args().Get(1))
	if err != nil {
		return fmt.Errorf("path: %w", err)
	}
	body, err := readAPIBody(cmd.String(flagData))
	if err != nil {
		return err
	}

	header, err := parseAPIHeaders(cmd.StringSlice(flagHeader))
	if err != nil {
		return err
	}

	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	out, err := c.RawWithHeaders(ctx, method, u.Path, u.Query(), body, header)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(out)
	if err == nil && len(out) > 0 && out[len(out)-1] != '\n' {
		_, err = fmt.Fprintln(os.Stdout)
	}
	return err
}

// parseAPIHeaders turns repeated -H "Name: value" flags into a header set.
func parseAPIHeaders(raw []string) (http.Header, error) {
	h := http.Header{}
	for _, r := range raw {
		name, value, ok := strings.Cut(r, ":")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			return nil, fmt.Errorf("%w: %q", errAPIHeader, r)
		}
		h.Add(name, strings.TrimSpace(value))
	}
	return h, nil
}

// readAPIBody resolves -d: a literal, @file, or @- for stdin.
func readAPIBody(v string) ([]byte, error) {
	switch {
	case v == "":
		return nil, nil
	case v == "@-":
		return io.ReadAll(os.Stdin)
	case strings.HasPrefix(v, "@"):
		return os.ReadFile(v[1:])
	default:
		return []byte(v), nil
	}
}
