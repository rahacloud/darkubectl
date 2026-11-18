package cmd

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rahacloud/darkubectl/internal/client"
)

func fakeJWT(exp time.Time) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d}`, exp.Unix())))
	return "header." + payload + ".signature"
}

func TestTUICredentialRefreshesOnceForConcurrentRequests(t *testing.T) {
	t.Parallel()
	now := time.Now()
	var calls atomic.Int32
	c := &tuiCredential{access: fakeJWT(now.Add(10 * time.Second)), refresh: "refresh-secret", now: func() time.Time { return now }}
	c.refreshAccess = func(context.Context, string) (string, error) {
		calls.Add(1)
		return fakeJWT(now.Add(time.Hour)), nil
	}
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := c.current(context.Background())
			if err != nil || got != client.BearerToken(fakeJWT(now.Add(time.Hour))) {
				t.Errorf("current() = %q, %v", got, err)
			}
		}()
	}
	wg.Wait()
	if got := calls.Load(); got != 1 {
		t.Fatalf("refresh calls = %d, want 1", got)
	}
}

func TestTUICredentialDoesNotRefreshExpiredExplicitToken(t *testing.T) {
	t.Parallel()
	now := time.Now()
	var calls atomic.Int32
	c := &tuiCredential{access: fakeJWT(now.Add(-time.Second)), now: func() time.Time { return now }}
	c.refreshAccess = func(context.Context, string) (string, error) { calls.Add(1); return "fallback", nil }
	if _, err := c.current(context.Background()); err == nil || !strings.Contains(err.Error(), "DARKUBE_ACCESS_TOKEN") {
		t.Fatalf("current() error = %v, want explicit token guidance", err)
	}
	if calls.Load() != 0 {
		t.Fatal("expired explicit token fell through to another credential")
	}
}

func TestTUICredentialRefreshFailureGivesRecoveryWithoutCredential(t *testing.T) {
	t.Parallel()
	now := time.Now()
	c := &tuiCredential{
		access: fakeJWT(now.Add(-time.Second)), refresh: "private-refresh-token", now: func() time.Time { return now },
		refreshAccess: func(context.Context, string) (string, error) {
			return "", errors.New("upstream echoed private-refresh-token")
		},
	}
	_, err := c.current(context.Background())
	if err == nil || !strings.Contains(err.Error(), "darkubectl login") || strings.Contains(err.Error(), "private-refresh-token") {
		t.Fatalf("refresh error should be actionable and redacted: %v", err)
	}
}
