package client_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rahacloud/darkubectl/internal/client"
)

// An invite is a PUT of the member list as read, every row echoed with the
// fields the client does not model, plus one {email} row per invitee.
func TestInviteMembersEchoesRowsAndAppends(t *testing.T) {
	t.Parallel()

	var put []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/organizations/7/members" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Method == http.MethodPut {
			data, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(data, &put)
			_, _ = w.Write([]byte(`[]`))
			return
		}
		_, _ = w.Write([]byte(`[{"id":1,"email":"a@example.com","roles":["admin"],"is_verified":true,"extra":"kept"}]`))
	}))
	defer srv.Close()

	c := client.New(srv.URL, client.APIKey("k"), "acme")
	if err := c.InviteMembers(context.Background(), 7, []string{"b@example.com", "c@example.com"}); err != nil {
		t.Fatalf("InviteMembers: %v", err)
	}
	if len(put) != 3 {
		t.Fatalf("PUT sent %d rows, want 3: %v", len(put), put)
	}
	if put[0]["extra"] != "kept" || put[0]["email"] != "a@example.com" {
		t.Errorf("existing row not echoed: %v", put[0])
	}
	if len(put[1]) != 1 || put[1]["email"] != "b@example.com" || put[2]["email"] != "c@example.com" {
		t.Errorf("invitee rows = %v, %v", put[1], put[2])
	}
}

func TestJoinOrganizationSendsTokenAndID(t *testing.T) {
	t.Parallel()

	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/organizations/join_by_token" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &body)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := client.New(srv.URL, client.APIKey("k"), "")
	if err := c.JoinOrganization(context.Background(), "tok", "22123"); err != nil {
		t.Fatalf("JoinOrganization: %v", err)
	}
	if body["token"] != "tok" || body["organization_id"] != "22123" {
		t.Errorf("body = %v", body)
	}
}

func TestProfileDecodesPendingInvitations(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"email":"me@example.com","organizations":[` +
			`{"id":7,"name":"acme","pending_invitation_emails":["b@example.com"],"invitation_emails":[]}]}`))
	}))
	defer srv.Close()

	c := client.New(srv.URL, client.APIKey("k"), "acme")
	org, err := c.Organization(context.Background())
	if err != nil {
		t.Fatalf("Organization: %v", err)
	}
	if len(org.PendingInvitations) != 1 || org.PendingInvitations[0] != "b@example.com" {
		t.Errorf("PendingInvitations = %v", org.PendingInvitations)
	}
}
