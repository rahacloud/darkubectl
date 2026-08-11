package client_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rahacloud/darkubectl/internal/client"
)

var errPlain = errors.New("plain")

// Callers branch on the API's error code because the accompanying `detail` is
// Persian prose. ErrorCode has to survive being wrapped with %w.
func TestErrorCode(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"detail":"اپی با همین نام وجود دارد.","success":false,"code":"SameHelmReleaseNameExists"}`))
	}))
	defer srv.Close()

	c := client.New(srv.URL, client.APIKey("k"), "acme")
	_, err := c.ListPlans(t.Context())
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := client.ErrorCode(err); got != client.CodeSameHelmReleaseName {
		t.Errorf("ErrorCode = %q, want %q", got, client.CodeSameHelmReleaseName)
	}
	if got := client.ErrorCode(fmt.Errorf("create failed: %w", err)); got != client.CodeSameHelmReleaseName {
		t.Errorf("ErrorCode through %%w = %q, want %q", got, client.CodeSameHelmReleaseName)
	}
	if got := client.ErrorCode(errPlain); got != "" {
		t.Errorf("ErrorCode(non-API error) = %q, want empty", got)
	}
}
