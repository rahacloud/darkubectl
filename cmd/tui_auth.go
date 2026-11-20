package cmd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rahacloud/darkubectl/internal/auth"
	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/rahacloud/darkubectl/internal/config"
	"github.com/urfave/cli/v3"
)

const (
	jwtExpiryValue3 = 3
)

const tuiRefreshBeforeExpiry = time.Minute

type tuiCredential struct {
	static        client.Auth
	access        string
	refresh       string
	baseURL       string
	now           func() time.Time
	refreshAccess func(context.Context, string) (string, error)
	mu            sync.Mutex
}

func newTUICredential(ctx context.Context, cmd *cli.Command, cfg *config.Config) (*tuiCredential, error) {
	c := &tuiCredential{baseURL: resolveBaseURL(cmd, cfg), now: time.Now}
	resolved, err := resolveAuth(ctx, cmd, cfg)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(string(resolved), "Api-key ") {
		c.static = resolved
	}
	if token, ok := strings.CutPrefix(string(resolved), "Bearer "); ok {
		c.access = token
	}
	if explicit := os.Getenv("DARKUBE_ACCESS_TOKEN"); explicit != "" {
		c.access = explicit
		c.refreshAccess = func(context.Context, string) (string, error) {
			return "", errors.New("explicit access token cannot be refreshed")
		}
	} else if cfg.RefreshToken != "" {
		c.refresh = cfg.RefreshToken
		c.refreshAccess = func(ctx context.Context, refresh string) (string, error) {
			a := auth.New(c.baseURL)
			defer func() { _ = a.Close() }()
			return a.Refresh(ctx, refresh)
		}
	}
	// An API key is the resolved REST credential, but a configured Console
	// refresh token can still supply the JWT needed by the pod websocket.
	if c.static != "" && c.access == "" && c.refresh == "" {
		return c, nil
	}
	if c.static == "" {
		if _, err := c.current(ctx); err != nil {
			return nil, err
		}
	} else if c.refresh != "" {
		if _, err := c.consoleAccessToken(ctx); err != nil {
			return nil, err
		}
	}
	return c, nil
}

func (c *tuiCredential) current(ctx context.Context) (client.Auth, error) {
	if c.static != "" {
		return c.static, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	token, err := c.currentAccessTokenLocked(ctx)
	if err != nil {
		return "", err
	}
	return client.BearerToken(token), nil
}

func (c *tuiCredential) currentAccessTokenLocked(ctx context.Context) (string, error) {
	if c.access != "" {
		if expiry, ok := jwtExpiry(c.access); ok && !expiry.After(c.now().Add(tuiRefreshBeforeExpiry)) {
			if c.refresh == "" {
				return "", errors.New("the explicit access token has expired; update DARKUBE_ACCESS_TOKEN and restart the TUI")
			}
			c.access = ""
		}
	}
	if c.access == "" {
		access, err := c.refreshAccess(ctx, c.refresh)
		if err != nil {
			return "", errors.New("could not refresh the TUI session; run `darkubectl login` and restart the TUI")
		}
		c.access = access
	}
	return c.access, nil
}

func (c *tuiCredential) consoleAccessToken(ctx context.Context) (string, error) {
	if c.static != "" && c.access == "" && c.refresh == "" {
		return "", errors.New("pod data requires a Console JWT; API-key authentication cannot open the pod stream")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	token, err := c.currentAccessTokenLocked(ctx)
	if err != nil {
		return "", err
	}
	if token == "" {
		return "", errors.New("pod data requires a Console JWT; API-key authentication cannot open the pod stream")
	}
	return token, nil
}

func jwtExpiry(token string) (time.Time, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != jwtExpiryValue3 {
		return time.Time{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, false
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(payload, &claims) != nil || claims.Exp == 0 {
		return time.Time{}, false
	}
	return time.Unix(claims.Exp, 0), true
}

func tuiAuthError(err error) error {
	if tuiCredentialRejected(err) {
		return errors.New("darkube rejected the TUI credential; run `darkubectl login` or update the configured token, then restart the TUI")
	}
	return err
}

func tuiCredentialRejected(err error) bool {
	var apiErr *client.APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnauthorized
}
