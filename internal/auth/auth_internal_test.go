package auth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fastRetry rewires a client for tests: same retry budget, no real sleeping.
func fastRetry(c *Client, attempts int) *Client {
	c.refreshAttempts = attempts
	c.refreshBackoff = time.Microsecond
	c.sleep = func(time.Duration) {}
	return c
}

func TestRefreshRetriesServerErrors(t *testing.T) {
	t.Parallel()

	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		// Two 500s, then success — the exact shape seen against the live API,
		// where the same command succeeded seconds after failing.
		if calls < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access":"recovered"}`)
	}))
	defer srv.Close()

	c := fastRetry(New(srv.URL), 4)
	access, err := c.Refresh(context.Background(), "ref")
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if access != "recovered" {
		t.Errorf("access = %q, want recovered", access)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3 (two failures then success)", calls)
	}
}

func TestRefreshRetriesTransportErrors(t *testing.T) {
	t.Parallel()

	var calls int
	// Hijack and drop the connection to produce the bare EOF the live endpoint
	// returns, which is a transport error rather than an HTTP status.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls < 2 {
			hijacker, ok := w.(http.Hijacker)
			if !ok {
				t.Error("ResponseWriter is not a Hijacker")
				return
			}
			conn, _, err := hijacker.Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			_ = conn.Close()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access":"after-eof"}`)
	}))
	defer srv.Close()

	c := fastRetry(New(srv.URL), 4)
	access, err := c.Refresh(context.Background(), "ref")
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if access != "after-eof" {
		t.Errorf("access = %q, want after-eof", access)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
}

func TestRefreshDoesNotRetryRejectedToken(t *testing.T) {
	t.Parallel()

	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"detail":"token not valid","code":"token_not_valid"}`)
	}))
	defer srv.Close()

	c := fastRetry(New(srv.URL), 4)
	_, err := c.Refresh(context.Background(), "expired")
	if !errors.Is(err, ErrRefreshFailed) {
		t.Fatalf("err = %v, want ErrRefreshFailed", err)
	}
	// An expired login never becomes valid by waiting, so retrying it would only
	// make the failure slower.
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (4xx must not be retried)", calls)
	}
	if strings.Contains(err.Error(), "attempts") {
		t.Errorf("err = %v, should not report a retry count for a fail-fast error", err)
	}
}

func TestRefreshGivesUpAfterBudget(t *testing.T) {
	t.Parallel()

	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := fastRetry(New(srv.URL), 3)
	_, err := c.Refresh(context.Background(), "ref")
	if !errors.Is(err, ErrRefreshFailed) {
		t.Fatalf("err = %v, want ErrRefreshFailed", err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
	if !strings.Contains(err.Error(), "3 attempts") {
		t.Errorf("err = %v, want it to report the attempt count", err)
	}
}

func TestRefreshDefaultsAreRetrying(t *testing.T) {
	t.Parallel()

	// Guards against a future refactor quietly dropping the retry budget back to
	// one attempt, which is the whole point of this change.
	c := New("https://example.invalid")
	if c.refreshAttempts < 2 {
		t.Errorf("refreshAttempts = %d, want >= 2 by default", c.refreshAttempts)
	}
	if c.refreshBackoff <= 0 {
		t.Errorf("refreshBackoff = %v, want > 0", c.refreshBackoff)
	}
}
