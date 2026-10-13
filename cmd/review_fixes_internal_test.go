package cmd

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/rahacloud/darkubectl/internal/wsexec"
)

func TestDescribeSecretChangeNamesWithoutValues(t *testing.T) {
	t.Parallel()

	before := []client.EnvVar{{Name: "A", Value: "1"}, {Name: "B", Value: "2"}, {Name: "C", Value: "3"}}
	after := []client.EnvVar{{Name: "A", Value: "1"}, {Name: "B", Value: "two"}, {Name: "D", Value: "4"}}
	got := describeSecretChange(before, after)
	if got != "add D; change B; remove C" {
		t.Errorf("describeSecretChange = %q", got)
	}
	for _, v := range []string{"two", "4", "3"} {
		if strings.Contains(got, v) {
			t.Errorf("describeSecretChange leaked value %q: %q", v, got)
		}
	}
	if got := describeSecretChange(before, before); got != "no change" {
		t.Errorf("unchanged = %q, want no change", got)
	}
}

func TestIsSessionEndMatchesWrappedExit(t *testing.T) {
	t.Parallel()

	if !isSessionEnd(wsexec.ErrExited) {
		t.Error("ErrExited should end the session")
	}
	if !isSessionEnd(errors.Join(errors.New("read"), wsexec.ErrExited)) {
		t.Error("a wrapped ErrExited should end the session")
	}
	if isSessionEnd(errors.New("connection reset")) {
		t.Error("an unrelated error is not a session end")
	}
}

func TestMatchAppInScopesToNamespace(t *testing.T) {
	t.Parallel()

	apps := []client.App{
		{ID: "1", Name: "api", Namespace: client.Namespace{ID: 10}},
		{ID: "2", Name: "api", Namespace: client.Namespace{ID: 20}},
	}
	got, err := matchAppIn(apps, "api", 20)
	if err != nil || got.ID != "2" {
		t.Fatalf("matchAppIn = %+v, %v; want app 2", got, err)
	}
	if _, err := matchAppIn(apps, "api", 30); !errors.Is(err, errNoAppInNamespace) {
		t.Errorf("err = %v, want errNoAppInNamespace", err)
	}
}

// TestApplyLookupListsOnce pins that apply resolves every document against one
// listing of the tenant, and lists again only after a create changed it.
func TestApplyLookupListsOnce(t *testing.T) {
	t.Parallel()

	var lists atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		lists.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"count":1,"next":null,"results":[{"id":"1","name":"api","namespace":{"id":10}}]}`))
	}))
	defer srv.Close()

	look := &applyLookup{c: client.New(srv.URL, client.APIKey("t"), "org")}
	ctx := context.Background()
	for range 3 {
		if _, err := look.appIn(ctx, "api", 10); err != nil {
			t.Fatalf("appIn: %v", err)
		}
	}
	if n := lists.Load(); n != 1 {
		t.Errorf("listed the tenant %d times for three lookups, want 1", n)
	}
	look.appsChanged()
	if _, err := look.appIn(ctx, "api", 10); err != nil {
		t.Fatalf("appIn: %v", err)
	}
	if n := lists.Load(); n != 2 {
		t.Errorf("listed %d times after a create, want 2", n)
	}
}

func TestRetryReadBackStopsOnSuccessAndHardErrors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var calls int
	ok, err := retryReadBack(ctx, func(context.Context) (bool, error) {
		calls++
		return true, nil
	})
	if !ok || err != nil || calls != 1 {
		t.Errorf("success: ok=%v err=%v calls=%d", ok, err, calls)
	}

	hard := &client.APIError{StatusCode: http.StatusForbidden}
	calls = 0
	_, err = retryReadBack(ctx, func(context.Context) (bool, error) {
		calls++
		return false, hard
	})
	if !errors.Is(err, hard) || calls != 1 {
		t.Errorf("a 403 should end the poll at once: err=%v calls=%d", err, calls)
	}
}

func TestNamedListDiffShowsEachVariable(t *testing.T) {
	t.Parallel()

	before := `[{"name":"A","value":"1"},{"name":"B","value":"2"},{"name":"C","value":"3"}]`
	after := `[{"name":"A","value":"1"},{"name":"B","value":"two"},{"name":"D","value":"4"}]`
	lines, ok := namedListDiff(before, after)
	if !ok {
		t.Fatal("envs should diff entry by entry")
	}
	if got := strings.Join(lines, "\n"); got != "~ B: 2 -> two\n+ D=4\n- C" {
		t.Errorf("diff =\n%s", got)
	}
	if lines, ok := namedListDiff("null", `[{"name":"A","value":"1"}]`); !ok || lines[0] != "+ A=1" {
		t.Errorf("from null: %v %v", lines, ok)
	}
	if _, ok := namedListDiff(`["a.example.com"]`, `["b.example.com"]`); ok {
		t.Error("a plain string list is not a name/value list")
	}
}
