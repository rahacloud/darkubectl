package cmd

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rahacloud/darkubectl/internal/client"
)

// appServer serves one canned app-detail response per request, in order, and
// repeats the last one forever.
func appServer(t *testing.T, responses ...func(w http.ResponseWriter)) *client.Client {
	t.Helper()
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		i := min(n, len(responses)-1)
		n++
		w.Header().Set("Content-Type", "application/json")
		responses[i](w)
	}))
	t.Cleanup(srv.Close)
	return client.New(srv.URL, client.APIKey("t"), "org")
}

func jsonBody(s string) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) { _, _ = io.WriteString(w, s) }
}

func status(code int) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) { w.WriteHeader(code) }
}

func TestCheckConditionReadyWhenHealthy(t *testing.T) {
	t.Parallel()

	c := appServer(t, jsonBody(`{"id":"x","state":{"state_type":"healthy","text":"healthy (1/1)"}}`))
	done, state, err := checkCondition(context.Background(), c, "x", condReady)
	if err != nil {
		t.Fatalf("checkCondition: %v", err)
	}
	if !done {
		t.Error("healthy app should satisfy --for ready")
	}
	if state != "healthy (1/1)" {
		t.Errorf("state = %q", state)
	}
}

func TestCheckConditionNotReadyWhileStarting(t *testing.T) {
	t.Parallel()

	c := appServer(t, jsonBody(`{"id":"x","state":{"state_type":"not_ready","text":"not ready (0/1)"}}`))
	done, state, err := checkCondition(context.Background(), c, "x", condReady)
	if err != nil {
		t.Fatalf("checkCondition: %v", err)
	}
	if done {
		t.Error("a not-ready app must not satisfy --for ready")
	}
	if state != "not ready (0/1)" {
		t.Errorf("state = %q", state)
	}
}

func TestCheckConditionDeletedOn404(t *testing.T) {
	t.Parallel()

	c := appServer(t, status(http.StatusNotFound))
	done, _, err := checkCondition(context.Background(), c, "x", condDeleted)
	if err != nil {
		t.Fatalf("checkCondition: %v", err)
	}
	if !done {
		t.Error("a 404 should satisfy --for deleted")
	}
}

func TestCheckConditionReadyFailsIfAppVanishes(t *testing.T) {
	t.Parallel()

	// Waiting for ready on an app that no longer exists can never succeed, so
	// failing immediately beats burning the whole timeout.
	c := appServer(t, status(http.StatusNotFound))
	if _, _, err := checkCondition(context.Background(), c, "x", condReady); err == nil {
		t.Error("expected an error when the app disappears while waiting for ready")
	}
}

func TestCheckConditionSwallowsTransientErrors(t *testing.T) {
	t.Parallel()

	// This API returns intermittent 5xx. A wait that aborts on one is worse than
	// useless, because the caller cannot tell it from a real failure.
	c := appServer(t, status(http.StatusInternalServerError))
	done, _, err := checkCondition(context.Background(), c, "x", condReady)
	if err != nil {
		t.Fatalf("a 5xx should be swallowed, got %v", err)
	}
	if done {
		t.Error("a 5xx must not be read as the condition being met")
	}
}

func TestCheckConditionSurfacesRealErrors(t *testing.T) {
	t.Parallel()

	// A 403 will not fix itself, so it should stop the wait rather than spin.
	c := appServer(t, status(http.StatusForbidden))
	if _, _, err := checkCondition(context.Background(), c, "x", condReady); err == nil {
		t.Error("expected a 403 to end the wait")
	}
}

func TestPollUntilReturnsWhenConditionMet(t *testing.T) {
	t.Parallel()

	c := appServer(t,
		jsonBody(`{"id":"x","state":{"state_type":"not_ready","text":"not ready (0/1)"}}`),
		jsonBody(`{"id":"x","state":{"state_type":"healthy","text":"healthy (1/1)"}}`),
	)
	err := pollUntil(context.Background(), c, "x", "api", condReady, time.Millisecond)
	if err != nil {
		t.Fatalf("pollUntil: %v", err)
	}
}

func TestPollUntilTimesOut(t *testing.T) {
	t.Parallel()

	c := appServer(t, jsonBody(`{"id":"x","state":{"state_type":"not_ready","text":"not ready (0/1)"}}`))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := pollUntil(ctx, c, "x", "api", condReady, time.Millisecond)
	if !errors.Is(err, errWaitTimeout) {
		t.Fatalf("err = %v, want errWaitTimeout", err)
	}
	// The message has to say what it was still waiting on, or the failure is
	// indistinguishable from a hang.
	if !strings.Contains(err.Error(), "not ready") {
		t.Errorf("timeout error should report the last state; got %v", err)
	}
}
