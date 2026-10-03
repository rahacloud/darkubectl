package client_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rahacloud/darkubectl/internal/client"
)

func TestRawWithHeadersSendsExtraHeaders(t *testing.T) {
	t.Parallel()

	var gotOTP, gotOrg, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotOTP = r.Header.Get("X-Otp")
		gotOrg = r.Header.Get("X-Organization")
		b, _ := io.ReadAll(r.Body)
		gotBody = strings.TrimSpace(string(b))
		_, _ = io.WriteString(w, `"ok"`)
	}))
	defer srv.Close()

	c := client.New(srv.URL, client.APIKey("tok"), "talaland")
	h := http.Header{}
	h.Set("X-Otp", "123456")
	out, err := c.RawWithHeaders(context.Background(), http.MethodPost, "/storage/v2/key/", nil, []byte(`{"name":"k"}`), h)
	if err != nil {
		t.Fatalf("RawWithHeaders: %v", err)
	}
	if string(out) != `"ok"` {
		t.Errorf("body = %s", out)
	}
	if gotOTP != "123456" || gotOrg != "talaland" || gotBody != `{"name":"k"}` {
		t.Errorf("otp=%q org=%q body=%q", gotOTP, gotOrg, gotBody)
	}
}
