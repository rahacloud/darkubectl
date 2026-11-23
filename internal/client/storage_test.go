package client_test

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

// TestStorageEndpoint pins how the S3 endpoint is chosen. It matters because
// the address cannot be derived from the tenant slug -- the platform name
// carries a random suffix, as in paradisehub-product-hhhwz.hs3.ir -- so this
// selection is the only way a caller learns where to point an S3 client.
func TestStorageEndpoint(t *testing.T) {
	t.Parallel()

	platform := client.StorageDomain{URL: "paradisehub-product-hhhwz.hs3.ir"}
	custom := client.StorageDomain{URL: "media.example.ir", Custom: true}

	for name, tc := range map[string]struct {
		domains []client.StorageDomain
		want    string
	}{
		"none":            {nil, ""},
		"platform only":   {[]client.StorageDomain{platform}, "https://paradisehub-product-hhhwz.hs3.ir"},
		"custom only":     {[]client.StorageDomain{custom}, "https://media.example.ir"},
		"custom wins":     {[]client.StorageDomain{platform, custom}, "https://media.example.ir"},
		"custom wins rev": {[]client.StorageDomain{custom, platform}, "https://media.example.ir"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := (client.Storage{Domains: tc.domains}).Endpoint(); got != tc.want {
				t.Errorf("Endpoint() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestStoragePlanAllowancesJoinsAssignments(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/storage/v2/storage/plans/":
			_, _ = w.Write([]byte(`[{"id":"private","current_plan_id":1},{"id":"missing","current_plan_id":9}]`))
		case "/storage/v2/billing/plans/":
			_, _ = w.Write([]byte(`[{"id":1,"disk_capacity":100,"traffic_capacity":400},{"id":2,"disk_capacity":500}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := client.New(srv.URL, client.BearerToken("t"), "acme")
	got, err := c.StoragePlanAllowances(context.Background())
	if err != nil || len(got) != 1 || got["private"].DiskGB == nil || *got["private"].DiskGB != 100 ||
		got["private"].TrafficGB == nil || *got["private"].TrafficGB != 400 {
		t.Fatalf("plan capacity join = %v, %v", got, err)
	}
}

func TestStorageTrafficRatesReadsLatestPerDomain(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/storage/v2/metrics/traffic" || r.URL.Query().Get("storage_id") != "s1" ||
			r.URL.Query().Get("start_time") == "" || r.URL.Query().Get("end_time") == "" {
			t.Errorf("unexpected traffic request: %s", r.URL.String())
		}
		_, _ = w.Write([]byte(`[{"id":"s1","domains":[{"url":"one.example","traffic_usage":[{"values":[` +
			`[100,"12"],[110,"20.5"],[115,"NaN"],[105,"5"]]}]},` +
			`{"url":"two.example","traffic_usage":[{"values":[[109,"30"]]}]}]},` +
			`{"id":"other","domains":[{"url":"wrong","traffic_usage":[{"values":[[120,"99"]]}]}]}]`))
	}))
	defer srv.Close()
	c := client.New(srv.URL, client.BearerToken("t"), "acme")
	samples, err := c.StorageTrafficRates(context.Background(), "s1", time.Unix(1, 0), time.Unix(200, 0))
	if err != nil || len(samples) != 2 || samples[0].Domain != "one.example" || samples[0].BytesPerSecond != 20.5 ||
		samples[0].ObservedAt.Unix() != 110 || samples[1].Domain != "two.example" || samples[1].BytesPerSecond != 30 {
		t.Fatalf("traffic samples = %+v, %v", samples, err)
	}
}

// The storage writes that touch a key or a bucket's visibility carry the
// account's two-factor code in x-otp, and a refusal for want of one has to come
// back as ErrOTPRequired so the command can ask for the code.
func TestStorageWritesSendTheOTP(t *testing.T) {
	t.Parallel()

	var calls []storageCall
	srv := httptest.NewServer(fakeStorage(&calls))
	defer srv.Close()

	c := client.New(srv.URL, client.BearerToken("t"), "acme")
	ctx := context.Background()

	if id, err := c.CreateBucket(ctx, "s-1", "media"); err != nil || id != "b-1" {
		t.Fatalf("CreateBucket = %q, %v", id, err)
	}
	if want := `{"is_public":false,"name":"media","storage_id":"s-1"}`; calls[0].body != want {
		t.Errorf("create body = %s, want %s", calls[0].body, want)
	}

	if err := c.SetBucketPublic(ctx, "b-1", true, ""); !errors.Is(err, client.ErrOTPRequired) {
		t.Errorf("a refused write: got %v, want ErrOTPRequired", err)
	}

	key, err := c.CreateStorageKey(ctx, "k", []string{client.StorageRead}, []string{"b-1"}, "123456")
	if err != nil || key.AccessKey != "AK" || key.SecretKey != "SK" || key.Name != "k" {
		t.Fatalf("CreateStorageKey = %+v, %v", key, err)
	}
	if err := c.UpdateStorageKey(ctx, "k", []string{client.StorageRead, client.StorageList}, []string{"b-1"}, "123456"); err != nil {
		t.Errorf("UpdateStorageKey: %v", err)
	}
	if _, err := c.RotateStorageKey(ctx, "k", "123456"); err != nil {
		t.Errorf("RotateStorageKey: %v", err)
	}
	if err := c.DeleteStorageKey(ctx, "k", "123456"); err != nil {
		t.Errorf("DeleteStorageKey: %v", err)
	}
	if err := c.DeleteBucket(ctx, "b-1", "123456"); err != nil {
		t.Errorf("DeleteBucket: %v", err)
	}

	want := []storageCall{
		{http.MethodPost, "/storage/v2/bucket/", "", ""},
		{http.MethodPost, "/storage/v2/bucket/b-1/visibility", "", ""},
		{http.MethodPost, "/storage/v2/key/", "123456", ""},
		{http.MethodPatch, "/storage/v2/key/", "123456", ""},
		{http.MethodPost, "/storage/v2/key/k/rotate", "123456", ""},
		{http.MethodDelete, "/storage/v2/key/k", "123456", ""},
		{http.MethodDelete, "/storage/v2/bucket/b-1", "123456", ""},
	}
	if len(calls) != len(want) {
		t.Fatalf("got %d calls, want %d: %+v", len(calls), len(want), calls)
	}
	for i, w := range want {
		if calls[i].method != w.method || calls[i].path != w.path || calls[i].otp != w.otp {
			t.Errorf("call %d = %s %s otp=%q, want %s %s otp=%q", i, calls[i].method, calls[i].path, calls[i].otp, w.method, w.path, w.otp)
		}
	}
}

type storageCall struct{ method, path, otp, body string }

// fakeStorage answers like the storage service: a bucket is created without a
// code, everything else is refused without one, and the two calls that mint a
// secret return the key with its pair.
func fakeStorage(calls *[]storageCall) http.Handler {
	const key = `{"name":"k","actions":["Read"],"buckets":["media"],"access_key":"AK","secret_key":"SK"}`
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		otp := r.Header.Get("X-Otp")
		*calls = append(*calls, storageCall{r.Method, r.URL.Path, otp, strings.TrimSpace(string(data))})
		mintsSecret := r.Method == http.MethodPost && (strings.HasSuffix(r.URL.Path, "/rotate") || r.URL.Path == "/storage/v2/key/")
		switch {
		case r.URL.Path == "/storage/v2/bucket/" && r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`"b-1"`))
		case otp == "":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"detail":"2fa code is not correct."}`))
		case mintsSecret:
			_, _ = w.Write([]byte(key))
		case r.Method == http.MethodPatch:
			_, _ = w.Write([]byte(`null`))
		}
	})
}

// A taken bucket name is a 409; the console's check-name route does not catch
// it, so the create's own answer is what gets translated.
func TestCreateBucketNameTaken(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"detail":"A bucket with this name already exists. Pick another name."}`))
	}))
	defer srv.Close()

	_, err := client.New(srv.URL, client.BearerToken("t"), "acme").CreateBucket(context.Background(), "s-1", "media")
	if !errors.Is(err, client.ErrBucketNameTaken) {
		t.Fatalf("got %v, want ErrBucketNameTaken", err)
	}
}
