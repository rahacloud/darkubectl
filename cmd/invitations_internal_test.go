package cmd

import (
	"errors"
	"testing"

	"github.com/rahacloud/darkubectl/internal/client"
)

func TestParseEmails(t *testing.T) {
	t.Parallel()

	got, err := parseEmails([]string{"a@example.com", "A@example.com", "b@example.com"})
	if err != nil || len(got) != 2 {
		t.Fatalf("parseEmails = %v, %v; want two addresses, duplicates folded", got, err)
	}
	for _, bad := range []string{"not-an-address", "Name <a@example.com>", ""} {
		if _, err := parseEmails([]string{bad}); err == nil {
			t.Errorf("parseEmails(%q) accepted", bad)
		}
	}
	if _, err := parseEmails(nil); !errors.Is(err, errMissingEmail) {
		t.Errorf("parseEmails(nil) = %v", err)
	}
}

func TestCheckInvitees(t *testing.T) {
	t.Parallel()

	members := []client.Member{{Email: "a@example.com"}}
	pending := []string{"b@example.com"}
	if err := checkInvitees([]string{"A@example.com"}, members, pending); err == nil {
		t.Error("a member was accepted as an invitee")
	}
	if err := checkInvitees([]string{"b@example.com"}, members, pending); err == nil {
		t.Error("an address with a pending invitation was accepted")
	}
	if err := checkInvitees([]string{"c@example.com"}, members, pending); err != nil {
		t.Errorf("new address refused: %v", err)
	}
}

func TestParseInvitationLink(t *testing.T) {
	t.Parallel()

	got, err := parseInvitationLink(
		" https://console.hamravesh.com/organization-invitation?token=abc&organization_id=22123&email=a%40example.com ")
	if err != nil || got != (inviteLink{token: "abc", orgID: "22123", email: "a@example.com"}) {
		t.Fatalf("parseInvitationLink = %+v, %v", got, err)
	}
	for _, bad := range []string{
		"https://console.hamravesh.com/organization-invitation?token=abc",
		"https://console.hamravesh.com/organization-invitation?organization_id=1",
		"https://console.hamravesh.com/organization-invitation?token=abc&organization_id=x",
	} {
		if _, err := parseInvitationLink(bad); !errors.Is(err, errBadInviteLink) {
			t.Errorf("parseInvitationLink(%q) = %v", bad, err)
		}
	}
}

func TestPendingInvitations(t *testing.T) {
	t.Parallel()

	got := pendingInvitations([]client.Organization{
		{Name: "acme", PendingInvitations: []string{"a@example.com", "b@example.com"}},
		{Name: "empty"},
	})
	if len(got) != 2 || got[0] != (invitation{Tenant: "acme", Email: "a@example.com"}) {
		t.Errorf("pendingInvitations = %v", got)
	}
	if got := pendingInvitations(nil); got == nil {
		t.Error("an empty result must be [] for -o json, not null")
	}
}

func TestReceivedInvitations(t *testing.T) {
	t.Parallel()

	const base = "https://console.hamravesh.com/organization-invitation?token="
	notes := []client.Notification{
		{ActionURL: base + "new&organization_name=talano&organization_id=10913&email=a%40example.com", Timestamp: "2026-11-28T08:00:27Z"},
		{ActionURL: "https://console.hamravesh.com/org-transfer?id=1"},
		{ActionURL: base + "old&organization_name=talano&organization_id=10913"},
		{ActionURL: base + "x&organization_name=acme&organization_id=22123"},
		{ActionURL: ""},
	}
	got := receivedInvitations(notes, []client.Organization{{ID: 22123, Name: "acme"}})
	if len(got) != 2 {
		t.Fatalf("receivedInvitations = %+v, want talano and acme once each", got)
	}
	if got[0].Tenant != "talano" || got[0].link.token != "new" || got[0].Joined || got[0].Email != "a@example.com" {
		t.Errorf("talano = %+v, want the newest link, not joined", got[0])
	}
	if !got[1].Joined {
		t.Errorf("acme = %+v, want joined", got[1])
	}
	if link, err := findReceivedInvitation(got, "TALANO"); err != nil || link.token != "new" {
		t.Errorf("findReceivedInvitation by name = %+v, %v", link, err)
	}
	if link, err := findReceivedInvitation(got, "22123"); err != nil || link.token != "x" {
		t.Errorf("findReceivedInvitation by id = %+v, %v", link, err)
	}
	if _, err := findReceivedInvitation(got, "nope"); !errors.Is(err, errNoReceivedInvite) {
		t.Errorf("findReceivedInvitation(nope) = %v", err)
	}
	if got := receivedInvitations(nil, nil); got == nil {
		t.Error("an empty result must be [] for -o json, not null")
	}
}
