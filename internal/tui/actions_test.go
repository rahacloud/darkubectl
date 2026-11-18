package tui

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rahacloud/darkubectl/internal/client"
)

func TestActionConfirmationCopyAndRestartFocusOrder(t *testing.T) {
	t.Parallel()
	m := &browser{width: 100, height: 30, mode: "LIVE", actionTargetName: "api", allowWrites: true, restartApp: func(string, string) error { return nil }}
	m.beginActionConfirmation(appActionRestart)
	if m.confirmButton != 0 {
		t.Fatal("Cancel should be focused first")
	}
	content, _, _, _ := m.actionConfirmationContent()
	copy := ansi.Strip(strings.Join(content, "\n"))
	for _, part := range []string{"↻ Restart", "Restart pods for api?", "[ Cancel ]", "[ ↻ Restart ]"} {
		if !strings.Contains(copy, part) {
			t.Errorf("restart confirmation missing %q: %s", part, copy)
		}
	}
	m.handleActionConfirmationKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.confirmAction != appActionNone {
		t.Fatal("Enter on the initially focused Cancel button should cancel")
	}
	m.beginActionConfirmation(appActionRestart)
	m.handleActionConfirmationKey(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.confirmButton != 1 {
		t.Fatal("Right should focus Restart")
	}
	m.handleActionConfirmationKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.confirmAction != appActionNone {
		t.Fatal("Esc should cancel restart confirmation")
	}
	m.beginActionConfirmation(appActionRestart)
	m.handleActionConfirmationKey(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if m.confirmAction != appActionNone {
		t.Fatal("n should take the No/Cancel path")
	}
}

func TestLiveActionsRequireWriteOptIn(t *testing.T) {
	t.Parallel()
	m := &browser{mode: "LIVE", restartApp: func(string, string) error { return nil }}
	if m.actionAvailable(appActionRestart) || m.actionAvailable(appActionDelete) {
		t.Fatal("live actions should be unavailable without write opt-in")
	}
	m.beginActionConfirmation(appActionRestart)
	if m.confirmAction != appActionNone || !strings.Contains(m.actionMenuMessage, "--allow-writes") {
		t.Fatalf("restart was not blocked with actionable guidance: action=%v message=%q", m.confirmAction, m.actionMenuMessage)
	}
	m.mode = "MOCK PROFILE · OFFLINE"
	if !m.actionAvailable(appActionRestart) || !m.actionAvailable(appActionDelete) {
		t.Fatal("mock actions should remain available without live-write opt-in")
	}
}

func TestDeleteRequiresExactNameAndSeparateClick(t *testing.T) {
	t.Parallel()
	var verified, deleted int
	m := &browser{
		width: 100, height: 30, mode: "LIVE", allowWrites: true, ctx: context.Background(),
		actionTargetTenant: "acme", actionTargetID: "id-1", actionTargetName: "api",
		getApp: func(_ context.Context, tenant, id string) (*client.App, error) {
			verified++
			if tenant != "acme" || id != "id-1" {
				t.Errorf("verified wrong target %s/%s", tenant, id)
			}
			return &client.App{ID: id, Name: "api"}, nil
		},
		deleteApp: func(tenant, id string) error { deleted++; return nil },
	}
	m.beginActionConfirmation(appActionDelete)
	content, _, _, _ := m.actionConfirmationContent()
	copy := ansi.Strip(strings.Join(content, "\n"))
	for _, part := range []string{"✖ Delete", "Type the exact app name:", "[ Cancel ]", "[ ✖ Delete ]", "Type name · click ✖ Delete"} {
		if !strings.Contains(copy, part) {
			t.Errorf("delete confirmation missing %q: %s", part, copy)
		}
	}
	if cmd := m.submitConfirmedAction(); cmd != nil || !strings.Contains(m.confirmError, "exactly") {
		t.Fatalf("empty name should block Delete; cmd=%v error=%q", cmd, m.confirmError)
	}
	m.handleActionConfirmationKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.confirmAction != appActionNone || deleted != 0 {
		t.Fatal("Esc should cancel Delete without calling the adapter")
	}
	m.beginActionConfirmation(appActionDelete)
	m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	m.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	m.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.confirmName != "api" || m.confirmButton != 1 {
		t.Fatalf("typed name/focus = %q/%d", m.confirmName, m.confirmButton)
	}
	_, deleteButton := m.actionConfirmButtonRects()
	cmd := m.handleActionMouse(tea.MouseClickMsg{X: deleteButton.left, Y: deleteButton.top, Button: tea.MouseLeft})
	if cmd == nil {
		t.Fatal("clicking the separate Delete button should submit after exact-name entry")
	}
	result, ok := cmd().(appActionResult)
	if !ok || result.err != nil {
		t.Fatalf("delete result = %#v", result)
	}
	if m.handleActionMouse(tea.MouseClickMsg{X: deleteButton.left, Y: deleteButton.top, Button: tea.MouseLeft}) != nil || m.submitConfirmedAction() != nil {
		t.Fatal("duplicate activation should not create a second mutation")
	}
	if verified != 1 || deleted != 1 {
		t.Fatalf("adapter calls verify=%d delete=%d, want 1 each", verified, deleted)
	}
}

func TestLiveRestartRevalidatesExactAppAndSubmitsOnce(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		current   *client.App
		writeErr  error
		wantCalls int
		wantError bool
	}{
		{name: "matching app", current: &client.App{ID: "id-1", Name: "api"}, wantCalls: 1},
		{name: "ambiguous write error is not retried", current: &client.App{ID: "id-1", Name: "api"}, writeErr: errors.New("connection closed after request"), wantCalls: 1, wantError: true},
		{name: "changed name", current: &client.App{ID: "id-1", Name: "other"}, wantError: true},
		{name: "changed ID", current: &client.App{ID: "id-2", Name: "api"}, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var verifyCalls, restartCalls int
			m := &browser{
				mode: "LIVE", allowWrites: true, ctx: context.Background(), confirmAction: appActionRestart,
				actionTargetTenant: "acme", actionTargetID: "id-1", actionTargetName: "api",
				getApp:     func(context.Context, string, string) (*client.App, error) { verifyCalls++; return tc.current, nil },
				restartApp: func(string, string) error { restartCalls++; return tc.writeErr },
			}
			cmd := m.submitConfirmedAction()
			if cmd == nil {
				t.Fatal("confirmed restart did not create its command")
			}
			result := cmd().(appActionResult)
			if (result.err != nil) != tc.wantError {
				t.Fatalf("result error = %v", result.err)
			}
			if m.submitConfirmedAction() != nil {
				t.Fatal("duplicate submit should be ignored")
			}
			m.applyActionResult(result)
			if verifyCalls != 1 || restartCalls != tc.wantCalls {
				t.Fatalf("verify=%d restart=%d, want 1/%d", verifyCalls, restartCalls, tc.wantCalls)
			}
		})
	}
}

func TestMockActionsStayInMemoryAndNeverCallLiveAdapters(t *testing.T) {
	path := "../../demo/mock-profile.yaml"
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := LoadMockProfile(path)
	if err != nil {
		t.Fatal(err)
	}
	m := newMockBrowser(profile)
	if cmd := m.Init(); cmd != nil {
		if result := cmd().(appsResult); result.err != nil {
			t.Fatal(result.err)
		} else {
			m.Update(result)
		}
	}
	var liveCalls int
	m.restartApp = func(string, string) error { liveCalls++; return nil }
	m.deleteApp = func(string, string) error { liveCalls++; return nil }
	app := m.apps[0]
	m.actionTargetTenant, m.actionTargetID, m.actionTargetName = m.tenant, app.ID, app.Name
	m.beginActionConfirmation(appActionRestart)
	restartCmd := m.submitConfirmedAction()
	restartResult := restartCmd().(appActionResult)
	if !restartResult.mock {
		t.Fatal("mock restart should use mock path")
	}
	m.applyActionResult(restartResult)
	if !strings.Contains(m.actionNotice, "simulated") {
		t.Fatalf("restart notice = %q", m.actionNotice)
	}

	m.beginActionConfirmation(appActionDelete)
	m.confirmName = app.Name
	deleteCmd := m.submitConfirmedAction()
	deleteResult := deleteCmd().(appActionResult)
	loadCmd := m.applyActionResult(deleteResult)
	if loadCmd == nil {
		t.Fatal("mock delete should reload the in-memory app list")
	}
	loaded := loadCmd().(appsResult)
	if loaded.err != nil {
		t.Fatal(loaded.err)
	}
	if len(loaded.apps) != len(profile.Apps)-1 {
		t.Fatalf("mock delete left %d apps, want %d", len(loaded.apps), len(profile.Apps)-1)
	}
	if liveCalls != 0 {
		t.Fatalf("mock mode called live adapters %d times", liveCalls)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("mock actions changed the fixture file")
	}
}
