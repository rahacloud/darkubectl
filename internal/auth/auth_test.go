package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rahacloud/darkubectl/internal/auth"
)

func TestLoginSendsCredentialsAndParsesTokens(t *testing.T) {
	t.Parallel()

	var gotOTP, gotPath string
	var body map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotOTP = r.Header.Get("X-Otp")
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access":"acc","refresh":"ref"}`)
	}))
	defer srv.Close()

	c := auth.New(srv.URL)
	toks, err := c.Login(context.Background(), "a@b.com", "pw", "123456")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if toks.Access != "acc" || toks.Refresh != "ref" {
		t.Errorf("tokens = %+v, want acc/ref", toks)
	}
	if gotPath != "/api/v1/token/" {
		t.Errorf("path = %q, want /api/v1/token/", gotPath)
	}
	if gotOTP != "123456" {
		t.Errorf("x-otp header = %q, want 123456", gotOTP)
	}
	if body["email"] != "a@b.com" || body["password"] != "pw" {
		t.Errorf("body = %v, want email/password", body)
	}
}

func TestRefreshParsesAccessToken(t *testing.T) {
	t.Parallel()

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access":"newacc"}`)
	}))
	defer srv.Close()

	c := auth.New(srv.URL)
	access, err := c.Refresh(context.Background(), "ref")
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if access != "newacc" {
		t.Errorf("access = %q, want newacc", access)
	}
	if gotPath != "/api/v1/token/refresh/" {
		t.Errorf("path = %q, want /api/v1/token/refresh/", gotPath)
	}
}

func TestLoginFailureWraps(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"detail":"invalid otp","code":"invalid"}`)
	}))
	defer srv.Close()

	c := auth.New(srv.URL)
	_, err := c.Login(context.Background(), "a@b.com", "pw", "000000")
	if !errors.Is(err, auth.ErrLoginFailed) {
		t.Errorf("err = %v, want ErrLoginFailed", err)
	}
}
