package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rahacloud/darkubectl/internal/client"
)

func TestNormalizeRange(t *testing.T) {
	t.Parallel()

	for _, tc := range [][2]string{
		{"203.0.113.30", "203.0.113.30/32"},
		{" 203.0.113.30/32\n", "203.0.113.30/32"},
		{"10.0.0.0/8", "10.0.0.0/8"},
		{"2001:db8::1", "2001:db8::1/128"},
		{"::ffff:1.2.3.4", "1.2.3.4/32"},
	} {
		if got, err := normalizeRange(tc[0]); err != nil || got != tc[1] {
			t.Errorf("normalizeRange(%q) = %q, %v; want %q", tc[0], got, err, tc[1])
		}
	}
	for _, in := range []string{"", "example.com", "1.2.3.4/33", "10.0.0.5/24", "fe80::1%eth0"} {
		if _, err := normalizeRange(in); !errors.Is(err, errBadRange) {
			t.Errorf("normalizeRange(%q): err = %v, want errBadRange", in, err)
		}
	}
}

func TestPlanWhitelist(t *testing.T) {
	t.Parallel()

	current := []string{"192.0.2.10/32", "198.51.100.20/32", "203.0.113.30/32"}
	cases := []struct {
		name                 string
		op                   whitelistOp
		args                 []string
		next, added, removed []string
		err                  error
	}{
		{name: "add present is a no-op", op: whitelistAdd, args: []string{"203.0.113.30"}, next: current},
		{
			name: "add keeps every entry and appends", op: whitelistAdd, args: []string{"203.0.113.7", "203.0.113.7/32"},
			next:  append(slices.Clone(current), "203.0.113.7/32"),
			added: []string{"203.0.113.7/32"},
		},
		{
			name: "remove one", op: whitelistRemove, args: []string{"198.51.100.20"},
			next:    []string{"192.0.2.10/32", "203.0.113.30/32"},
			removed: []string{"198.51.100.20/32"},
		},
		{name: "remove absent is refused", op: whitelistRemove, args: []string{"203.0.113.7"}, err: errNotInWhitelist},
		{
			name: "remove everything is refused", op: whitelistRemove,
			args: current, err: errEmptyWhitelist,
		},
		{
			name: "set replaces", op: whitelistSet, args: []string{"192.0.2.10", "203.0.113.0/24"},
			next:    []string{"192.0.2.10/32", "203.0.113.0/24"},
			added:   []string{"203.0.113.0/24"},
			removed: []string{"198.51.100.20/32", "203.0.113.30/32"},
		},
		{name: "set to the same is a no-op", op: whitelistSet, args: []string{"203.0.113.30", "192.0.2.10", "198.51.100.20"},
			next: []string{"203.0.113.30/32", "192.0.2.10/32", "198.51.100.20/32"}},
		{name: "bad input", op: whitelistAdd, args: []string{"nope"}, err: errBadRange},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			next, added, removed, err := planWhitelist(current, tc.op, tc.args)
			if tc.err != nil {
				if !errors.Is(err, tc.err) {
					t.Fatalf("err = %v, want %v", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(next, tc.next) || !slices.Equal(added, tc.added) || !slices.Equal(removed, tc.removed) {
				t.Errorf("got next=%q added=%q removed=%q; want %q %q %q", next, added, removed, tc.next, tc.added, tc.removed)
			}
		})
	}
	if current[1] != "198.51.100.20/32" {
		t.Error("planWhitelist modified the list it was given")
	}
}

// fakeWhitelist serves one cluster's whitelist, storing what is posted and
// counting the writes.
type fakeWhitelist struct {
	mu     sync.Mutex
	ranges []string
	posts  int
}

func (f *fakeWhitelist) serve(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/podolica/v1/clusters/c216/api-server-whitelist/" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Method == http.MethodPost {
			f.posts++
			data, _ := io.ReadAll(r.Body)
			var body map[string][]string
			_ = json.Unmarshal(data, &body)
			f.ranges = body["source_ranges"]
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"source_ranges": f.ranges, "synced_at": time.Now().Format(time.RFC3339)})
	}))
}

// The add path sends the whole list back with the new entry, and an add of
// what is already there, or a dry run, sends nothing at all.
func TestChangeWhitelist(t *testing.T) {
	t.Parallel()

	f := &fakeWhitelist{ranges: []string{"192.0.2.10/32", "203.0.113.30/32"}}
	srv := f.serve(t)
	defer srv.Close()
	c := client.New(srv.URL, client.APIKey("k"), "acme")
	ctx := context.Background()

	if err := changeWhitelist(ctx, c, "c216", whitelistAdd, []string{"203.0.113.30"}, false, true); err != nil {
		t.Fatal(err)
	}
	if err := changeWhitelist(ctx, c, "c216", whitelistAdd, []string{"203.0.113.7"}, true, true); err != nil {
		t.Fatal(err)
	}
	if f.posts != 0 {
		t.Fatalf("a no-op and a dry run posted %d times", f.posts)
	}

	if err := changeWhitelist(ctx, c, "c216", whitelistAdd, []string{"203.0.113.7"}, false, true); err != nil {
		t.Fatal(err)
	}
	want := []string{"192.0.2.10/32", "203.0.113.30/32", "203.0.113.7/32"}
	if f.posts != 1 || !slices.Equal(f.ranges, want) {
		t.Errorf("after add: posts=%d ranges=%q, want 1 and %q", f.posts, f.ranges, want)
	}

	if err := changeWhitelist(ctx, c, "c216", whitelistRemove, []string{"203.0.113.7/32"}, false, true); err != nil {
		t.Fatal(err)
	}
	if f.posts != 2 || !slices.Equal(f.ranges, want[:2]) {
		t.Errorf("after remove: posts=%d ranges=%q, want 2 and %q", f.posts, f.ranges, want[:2])
	}

	err := changeWhitelist(ctx, c, "nope", whitelistAdd, []string{"203.0.113.7"}, false, true)
	if err == nil || !strings.Contains(err.Error(), `no KaaS cluster "nope"`) {
		t.Errorf("unknown cluster: err = %v", err)
	}
}

func TestBackupFreshness(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	b := func(date, status string) client.EtcdBackup { return client.EtcdBackup{Date: date, Status: status} }
	cases := []struct {
		name    string
		backups []client.EtcdBackup
		want    string
	}{
		{"fresh", []client.EtcdBackup{b("2026-11-18T06:00:00+00:00", "success")}, ""},
		{"stale", []client.EtcdBackup{b("2026-11-03T06:00:07+00:00", "success")}, "15 days old"},
		{"only failures are fresh", []client.EtcdBackup{
			b("2026-11-18T06:00:00+00:00", "failed"), b("2026-11-10T06:00:00+00:00", "success"),
		}, "8 days old"},
		{"none", nil, "no successful etcd backup"},
	}
	for _, tc := range cases {
		got := backupFreshness(tc.backups, now)
		if (tc.want == "") != (got == "") || !strings.Contains(got, tc.want) {
			t.Errorf("%s: %q, want one containing %q", tc.name, got, tc.want)
		}
	}
}

// A kubeconfig is a credential: it must be written 0600, must not replace a
// file unless forced, and a forced write must not keep a looser mode.
func TestWriteKubeconfig(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "kc.yaml")
	if err := writeKubeconfig(path, []byte("a"), false); err != nil {
		t.Fatal(err)
	}
	if err := writeKubeconfig(path, []byte("b"), false); !errors.Is(err, errKubeconfigExist) {
		t.Errorf("second write: err = %v, want errKubeconfigExist", err)
	}
	if err := os.Chmod(path, 0o644); err != nil { //nolint:gosec // loosened on purpose
		t.Fatal(err)
	}
	if err := writeKubeconfig(path, []byte("c"), true); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path) //nolint:gosec // a file in t.TempDir
	if string(data) != "c" || info.Mode().Perm() != kubeconfigPerm {
		t.Errorf("forced write left %q with mode %o", data, info.Mode().Perm())
	}
}
