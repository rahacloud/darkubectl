package browser

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rahacloud/darkubectl/internal/client"
)

func storageRegressionBrowser() *browser {
	return &browser{page: pageStorage, width: 120, height: 30, ctx: context.Background(), mode: "LIVE", tenant: "test",
		allowWrites: true, allowSecrets: true, storageState: requestState{status: requestReady}, storageSelectedID: "s",
		storageKind: storageViewKeys, storageRows: []client.Storage{{ID: "s", Name: "Example", Buckets: []client.Bucket{
			{ID: "media-id", Name: "media", Status: "active"}, {ID: "logs-id", Name: "logs", Status: "active"}},
			Keys: []client.StorageKey{{Name: "reader", Actions: []string{client.StorageRead}, Buckets: []string{"media"}}}}}}
}

func TestStoragePasteRoutesToActiveFieldAndMasksOTP(t *testing.T) {
	t.Parallel()
	m := storageRegressionBrowser()
	m.handleKeyPress(tea.KeyPressMsg{Code: 'K', Text: "K"})
	m.Update(tea.PasteMsg{Content: "reader-copy"})
	if m.storageDraft.text != "reader-copy" {
		t.Fatal("key name paste was lost")
	}
	m.storageDraft.permissions, m.storageDraft.buckets = "R", "media"
	m.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(tea.PasteMsg{Content: " 123456\n"})
	if m.storageDraft.otp != "123456" {
		t.Fatal("pasted authenticator code was lost")
	}
	if strings.Contains(strings.Join(m.storageDialogLines(), "\n"), "123456") {
		t.Fatal("authenticator code was rendered")
	}
	m.Update(tea.PasteMsg{Content: "bad-code"})
	if m.storageDraft.otp != "123456" {
		t.Fatal("invalid paste changed the authenticator code")
	}
	m.storageDraft.otp = "12"
	if m.advanceStorageDialog() != nil || m.storageBusy {
		t.Fatal("short code was submitted")
	}
}

func TestStorageGrantCheckboxesPreserveExistingGrants(t *testing.T) {
	t.Parallel()
	m := storageRegressionBrowser()
	m.storageSelectedChild = "reader"
	m.handleKeyPress(tea.KeyPressMsg{Code: 'g', Text: "g"})
	if m.storageDialog != storageDialogKeyForm || m.storageDraft.text != "reader" {
		t.Fatal("grant form did not open")
	}
	m.storageDraft.field = 1
	m.handleKeyPress(tea.KeyPressMsg{Code: ' ', Text: " "})
	m.storageDraft.field = 4
	m.handleKeyPress(tea.KeyPressMsg{Code: ' ', Text: " "})
	if m.storageDraft.permissions != "R" || m.storageDraft.buckets != "media" {
		t.Fatal("existing grant was removed")
	}
	m.storageDraft.field = 2
	m.handleKeyPress(tea.KeyPressMsg{Code: ' ', Text: " "})
	m.storageDraft.field = 5
	m.handleKeyPress(tea.KeyPressMsg{Code: ' ', Text: " "})
	m.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.handlePaste("123456")
	m.listStorages = func(context.Context, string) ([]client.Storage, error) { return cloneMockStorages(m.storageRows), nil }
	called := false
	m.updateStorageKey = func(_ context.Context, tenant, name string, actions, buckets []string, otp string) error {
		called = true
		if tenant != "test" || name != "reader" || !sameStorageSet(actions, []string{client.StorageRead, client.StorageWrite}) ||
			!sameStorageSet(buckets, []string{"media-id", "logs-id"}) || otp != "123456" {
			t.Error("grant callback received incorrect values")
		}
		return nil
	}
	_, cmd := m.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("review did not submit")
	}
	result, ok := cmd().(storageWriteResult)
	if !ok {
		t.Fatal("unexpected command result")
	}
	if !called || result.err != nil {
		t.Fatalf("grant failed: %v", result.err)
	}
}

func TestStorageKeyFormFollowsFocusedField(t *testing.T) {
	t.Parallel()
	m := storageRegressionBrowser()
	m.storageDialog = storageDialogKeyForm
	for i := range 30 {
		m.storageRows[0].Buckets = append(m.storageRows[0].Buckets, client.Bucket{Name: strings.Repeat("x", i+1)})
	}
	for _, field := range []int{0, 1, 3, 4, 20, 30, 1} {
		m.storageDraft.field = field
		m.storageDetailViewport(m.storageKeyFormLines(), 6)
		row := storageFormFieldRow(field)
		if row < m.storageDetailScroll || row >= m.storageDetailScroll+6 {
			t.Errorf("field %d is outside viewport", field)
		}
	}
	m.storageDraft.field = 20
	m.storageDetailViewport(m.storageKeyFormLines(), 3)
	if storageFormFieldRow(20) >= m.storageDetailScroll+3 {
		t.Fatal("resize hid focused checkbox")
	}
}

func TestStorageSaveReviewFreezesDestinationAndRecovers(t *testing.T) {
	t.Parallel()
	m := storageRegressionBrowser()
	m.storageSecret = &client.StorageKeySecret{AccessKey: "fake-access", SecretKey: "fake-secret"}
	m.handleKeyPress(tea.KeyPressMsg{Code: 's', Text: "s"})
	path := filepath.Join(t.TempDir(), "credentials.json")
	m.handlePaste(path)
	m.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.storageDialog != storageDialogSaveReview {
		t.Fatal("save review did not open")
	}
	m.handleKeyPress(tea.KeyPressMsg{Code: 'x', Text: "x"})
	m.handlePaste("different.env")
	if m.storageDraft.text != path || m.storageDraft.exportPath != path {
		t.Fatal("confirmation changed reviewed destination")
	}
	if err := os.WriteFile(path, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.storageDialog != storageDialogSavePath || m.storageSecret == nil {
		t.Fatal("destination conflict did not return to editing")
	}
	//nolint:gosec // The path is inside t.TempDir.
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "existing" {
		t.Fatal("existing file was modified")
	}
}

func TestStorageBucketVerificationPollsWithoutRepeatingWrite(t *testing.T) {
	t.Parallel()
	m := storageRegressionBrowser()
	m.storageKind = storageViewBuckets
	m.storagePendingAction = storageOpBucketCreate
	m.captureStorageTarget()
	m.storageDraft.name = "new-bucket"
	creates := 0
	m.createStorageBucket = func(context.Context, string, string, string) (string, error) { creates++; return "new-id", nil }
	m.listStorages = func(context.Context, string) ([]client.Storage, error) { return nil, nil }
	cmd := m.submitStorageWrite()
	if cmd == nil {
		t.Fatal("creation did not submit")
	}
	result, ok := cmd().(storageWriteResult)
	if !ok {
		t.Fatal("unexpected command result")
	}
	m.handleStorageWriteResult(result)
	rows := []client.Storage{{ID: "s", Buckets: []client.Bucket{{ID: "new-id", Name: "new-bucket", Status: "creating"}}}}
	_, poll, _ := m.handleStorageDataResult(storageSnapshot{meta: m.storageState.meta, rows: rows})
	if poll == nil || m.storageOutcome != storageOutcomeVerifying || m.storageVerifyAction == "" {
		t.Fatal("creating bucket was not followed")
	}
	m.handleStorageDataResult(storageVerificationTick{tenant: "test", generation: m.tenantGeneration, id: m.storageVerifyID})
	rows[0].Buckets[0].Status = "active"
	m.handleStorageDataResult(storageSnapshot{meta: m.storageState.meta, rows: rows})
	if m.storageOutcome != storageOutcomeVerified || m.storageVerifyAction != "" || creates != 1 {
		t.Fatal("readiness did not finish with one creation request")
	}
}

func TestStorageVerificationTimeoutAndStaleTick(t *testing.T) {
	t.Parallel()
	m := storageRegressionBrowser()
	m.storageVerifyAction, m.storageVerifyStorage, m.storageVerifyName = storageOpBucketCreate, "s", "pending"
	m.storageVerifyAttempts = storageVerificationLimit - 1
	meta := requestMeta{tenant: "test", scopeGeneration: m.tenantGeneration, requestID: 1}
	m.storageState.begin(meta, m.requestTime())
	m.handleStorageSnapshot(storageSnapshot{meta: meta, rows: []client.Storage{{ID: "s"}}})
	if m.storageOutcome != storageOutcomeUnknown || m.storageVerifyAction != "" {
		t.Fatal("readiness did not time out")
	}
	_, cmd, _ := m.handleStorageDataResult(storageVerificationTick{tenant: "old-tenant", id: m.storageVerifyID})
	if cmd != nil {
		t.Fatal("stale tenant verification ran")
	}
}

func TestStorageDefiniteRejectionAndFeedbackAreDistinct(t *testing.T) {
	t.Parallel()
	m := storageRegressionBrowser()
	m.listStorages = func(context.Context, string) ([]client.Storage, error) { return nil, nil }
	m.handleStorageWriteResult(storageWriteResult{tenant: "test", action: storageOpKeyCreate,
		err: &client.APIError{StatusCode: http.StatusBadRequest, Detail: "invalid name"}})
	if m.storageOutcome != storageOutcomeRejected || strings.Contains(m.storageNotice, "may have succeeded") {
		t.Fatal("definite rejection reported as uncertain")
	}
	m.storageFeedbackMessage("Credential pair copied")
	m.handleStorageWriteResult(storageWriteResult{tenant: "test", action: storageOpKeyCreate, err: errTestStorageGeneric})
	if m.storageOutcome != storageOutcomeUnknown || m.storagePaneStatus() != "Credential pair copied" {
		t.Fatal("background outcome replaced copy feedback")
	}
	m.handleStorageDataResult(storageFeedbackTimeout{id: m.storageFeedbackID})
	if m.storagePaneStatus() == "Credential pair copied" {
		t.Fatal("feedback did not expire")
	}
}

func TestStorageUnrelatedDraftGrantsCannotBlockDeletion(t *testing.T) {
	t.Parallel()
	m := storageRegressionBrowser()
	m.storageKind, m.storageSelectedChild = storageViewBuckets, "media-id"
	m.storagePendingAction = storageOpBucketDelete
	m.captureStorageTarget()
	m.storageDraft.permissions, m.storageDraft.buckets, m.storageDraft.otp = "invalid", "missing", "123456"
	called := false
	m.deleteStorageBucket = func(context.Context, string, string, string) error { called = true; return nil }
	cmd := m.submitStorageWrite()
	if cmd == nil {
		t.Fatal("leftover grants blocked deletion")
	}
	result, ok := cmd().(storageWriteResult)
	if !ok || result.err != nil || !called {
		t.Fatal("deletion was not called")
	}
}

func TestStorageDataDiagnosticsDistinguishMissingAndFailure(t *testing.T) {
	t.Parallel()
	m := storageRegressionBrowser()
	m.storageTrafficID = "s"
	empty := ansi.Strip(strings.Join(m.appendStorageTrafficDetails(nil, "s"), "\n"))
	m.storageTrafficState.fail(errTestStorageGeneric)
	failed := ansi.Strip(strings.Join(m.appendStorageTrafficDetails(nil, "s"), "\n"))
	if empty == failed || !strings.Contains(empty, "No samples") || !strings.Contains(failed, "lookup failed") {
		t.Fatal("traffic states are indistinguishable")
	}
}

func TestMockStorageChangesSurviveRefresh(t *testing.T) {
	t.Parallel()
	fixture := []client.Storage{{ID: "s", Name: "Example"}}
	m := &browser{tenant: "test", page: pageStorage, mode: "MOCK", storageSelectedID: "s",
		storageKind: storageViewBuckets, storageState: requestState{status: requestReady}, storageRows: cloneMockStorages(fixture)}
	state := &mockBrowserData{storagesByTenant: map[string][]client.Storage{"test": cloneMockStorages(fixture)}}
	setupMockStorageCallbacks(m, state)
	m.storagePendingAction = storageOpBucketCreate
	m.captureStorageTarget()
	m.storageDraft.name = "created"
	m.submitStorageWrite()
	refreshed, err := m.listStorages(context.Background(), "test")
	if err != nil || len(refreshed[0].Buckets) != 1 || refreshed[0].Buckets[0].Name != "created" {
		t.Fatal("refresh lost mock change")
	}
	if len(fixture[0].Buckets) != 0 {
		t.Fatal("fixture was mutated")
	}
}

func TestStorageInvalidCodeRetriesOnReviewWithoutReplay(t *testing.T) {
	t.Parallel()
	m := storageRegressionBrowser()
	m.storagePendingAction, m.storagePendingStorage = storageOpKeyCreate, "s"
	m.storageDraft.name, m.storageDraft.permissions, m.storageDraft.buckets = "new-key", "R", "media"
	m.listStorages = func(context.Context, string) ([]client.Storage, error) { return nil, nil }
	m.handleStorageWriteResult(storageWriteResult{tenant: "test", action: storageOpKeyCreate, err: client.ErrOTPRequired})
	if m.storageDialog != storageDialogWriteConfirm || m.storageOutcome != storageOutcomeRejected || m.storageDraft.otp != "" {
		t.Fatal("invalid code did not retain the reviewed draft")
	}
	if cmd := m.advanceStorageDialog(); cmd != nil {
		t.Fatal("rejected operation was automatically replayed")
	}
}

func TestStorageActionModalsFitSupportedSizes(t *testing.T) {
	t.Parallel()
	for _, width := range []int{60, 80, 120} {
		for _, dialog := range []string{storageDialogVisibilityConfirm, storageDialogBucketDeleteConfirm,
			storageDialogOTP, storageDialogSavePath} {
			m := storageRegressionBrowser()
			m.width, m.height = width, 20
			m.storageDialog, m.storagePendingAction = dialog, storageOpVisibility
			m.storageKind, m.storageSelectedChild = storageViewBuckets, "media-id"
			if dialog == storageDialogSavePath {
				m.storageDraft.text = "/tmp/" + strings.Repeat("long-path/", 20) + "key.json"
			}
			view := m.View().Content
			if len(strings.Split(view, "\n")) != m.height {
				t.Errorf("modal %s changed terminal height", dialog)
			}
			for line := range strings.SplitSeq(view, "\n") {
				if ansi.StringWidth(line) > width {
					t.Errorf("modal %s exceeded width %d", dialog, width)
				}
			}
		}
	}
}

func TestStorageRemovedPlanAssignmentClearsAllowance(t *testing.T) {
	t.Parallel()
	m := storageRegressionBrowser()
	allowance := 100.0
	m.storageRows[0].PlanDiskGB = &allowance
	meta := requestMeta{tenant: "test", requestID: 1}
	m.storagePlansState.begin(meta, m.requestTime())
	m.handleStoragePlansSnapshot(storagePlansSnapshot{meta: meta, allowances: map[string]client.StoragePlanAllowance{}})
	if m.storageRows[0].PlanDiskGB != nil {
		t.Fatal("removed plan kept its old allowance")
	}
}

func TestStorageCompactLayoutKeepsRightBorderVisible(t *testing.T) {
	t.Parallel()
	for _, width := range []int{60, 80, 99} {
		m := storageRegressionBrowser()
		m.width = width
		lines := m.splitDatabaseCompact(nil, nil, 18)
		if !strings.HasSuffix(ansi.Strip(lines[0]), "╮") || !strings.HasSuffix(ansi.Strip(lines[len(lines)-1]), "╯") {
			t.Errorf("width %d lost right-hand pane border", width)
		}
		for _, line := range lines {
			if ansi.StringWidth(line) != width {
				t.Errorf("width %d: rendered %d columns", width, ansi.StringWidth(line))
			}
		}
	}
}

func TestStorageCheckboxMouseUsesScrolledPaneGeometry(t *testing.T) {
	t.Parallel()
	for _, width := range []int{80, 140} {
		m := storageRegressionBrowser()
		m.width, m.detail, m.storageDialog = width, true, storageDialogKeyForm
		m.storageDraft.field = 5
		m.storageDetailViewport(m.storageKeyFormLines(), 8)
		listWidth, _, contextWidth := m.paneWidths()
		x := contextWidth + 5
		if width >= viewValue100 {
			x += listWidth + 3
		}
		y := m.browserBodyY() + 1 + storageFormFieldRow(4) - m.storageDetailScroll
		m.handleBrowserMouse(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
		if m.storageDraft.field != 4 || m.storageDraft.buckets != "media" {
			t.Errorf("width %d clicked the wrong checkbox", width)
		}
	}
}
