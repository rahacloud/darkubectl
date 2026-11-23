package browser

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rahacloud/darkubectl/internal/client"
)

var (
	errTestStorageOTPRejected = errors.New("2fa code was rejected")
	errTestStorageGeneric     = errors.New("permission denied")
)

func TestStorageTreeAndCredentialMasking(t *testing.T) {
	t.Parallel()
	m := &browser{
		page: pageStorage, width: 140, tenant: "fictional", storageKind: storageViewBuckets,
		storageState: requestState{status: requestReady}, storageSelectedID: "s1",
		storageExpanded: true, storageScroll: 0, storageSelectedChild: "b1",
		storageRows: []client.Storage{{
			ID: "s1", Name: "Example storage", Buckets: []client.Bucket{{ID: "b1", Name: "media", Status: "active", Size: "1073741824"}},
		}},
		storageSecret: &client.StorageKeySecret{Name: "reader",
			AccessKey: "access-secret-value", SecretKey: "secret-secret-value"},
	}
	rows := strings.Join(m.storagePaneLines(8), "\n")
	if !strings.Contains(rows, "▾ "+storageIconDatacenter+" Example storage") ||
		!strings.Contains(rows, "└── "+storageIconBucket+" media") ||
		!strings.Contains(rows, "1.0 GiB") {
		t.Fatalf("bucket tree is missing expected rows: %q", rows)
	}
	details := strings.Join(m.storageDetailLines(20), "\n")
	if strings.Contains(details, "access-secret-value") || strings.Contains(details, "secret-secret-value") {
		t.Fatal("credentials appeared while masked")
	}
	m.storageSecretVisible = true
	details = strings.Join(m.storageDetailLines(20), "\n")
	if !strings.Contains(details, "access-secret-value") || !strings.Contains(details, "secret-secret-value") {
		t.Fatal("explicit reveal did not render credentials")
	}
}

func TestStorageTreeCollapseEmptyAndStableChildSelection(t *testing.T) {
	t.Parallel()
	m := &browser{
		width: 80,
		storageRows: []client.Storage{{
			ID: "s1", Name: "Empty", Buckets: []client.Bucket{},
		}},
		storageSelectedID: "s1", storageKind: storageViewBuckets, storageExpanded: true,
	}
	rows := strings.Join(m.storagePaneLines(8), "\n")
	if !strings.Contains(rows, "(no buckets)") {
		t.Fatalf("empty storage branch is missing: %q", rows)
	}
	m.storageExpanded = false
	rows = strings.Join(m.storagePaneLines(8), "\n")
	if strings.Contains(rows, "(no buckets)") || !strings.Contains(rows, "▸ "+storageIconDatacenter+" Empty") {
		t.Fatalf("collapsed storage still shows children: %q", rows)
	}

	m.storageRows = []client.Storage{{ID: "s1", Name: "Storage", Buckets: []client.Bucket{
		{ID: "b2", Name: "new"}, {ID: "b1", Name: "selected"},
	}}}
	m.storageExpanded = true
	m.storageScroll = 0
	m.syncStorageChildSelection(&m.storageRows[0], "b1")
	if m.storageScroll != 1 || m.selectedBucket() == nil || m.selectedBucket().ID != "b1" {
		t.Fatal("refresh did not preserve the selected bucket by stable ID")
	}
	m.syncStorageChildSelection(&m.storageRows[0], "removed")
	if m.selectedBucket() != nil {
		t.Fatal("missing selected bucket silently selected a different bucket")
	}
}

func TestStorageTreeAlignsUsageAndCounts(t *testing.T) {
	t.Parallel()
	allowance := 100.0
	m := &browser{storageKind: storageViewBuckets, storageRows: []client.Storage{
		{ID: "a", Name: "Asiatech-storage", PlanDiskGB: &allowance, Buckets: []client.Bucket{{ID: "b", Size: "1073741824"}}},
		{ID: "b", Name: "Qom - Amin"},
	}}
	first := ansi.Strip(m.storageTreeLine(storageTreeItem{storage: 0, child: -1}, 60))
	second := ansi.Strip(m.storageTreeLine(storageTreeItem{storage: 1, child: -1}, 60))
	if strings.Contains(first+second, "/ —") || !strings.Contains(first, "1.0 GiB / 100 GB") || !strings.Contains(second, "0 buckets") {
		t.Fatalf("unexpected storage rows: %q %q", first, second)
	}
	firstIndex, secondIndex := strings.Index(first, "1.0 GiB"), strings.Index(second, "0 B")
	if firstIndex < 0 || secondIndex < 0 {
		t.Fatalf("missing usage values: %q %q", first, second)
	}
	firstUsage := ansi.StringWidth(first[:firstIndex])
	secondUsage := ansi.StringWidth(second[:secondIndex])
	if firstUsage != secondUsage {
		t.Fatalf("usage columns do not align: %q %q", first, second)
	}
}

func TestStoragePlansLoadSeparatelyAndRejectLateTenantResults(t *testing.T) {
	t.Parallel()
	capacity := 100.0
	m := &browser{page: pageStorage, tenant: "a", tenantGeneration: 1, storageRows: []client.Storage{{ID: "private"}},
		getStoragePlans: func(_ context.Context, _ string) (map[string]client.StoragePlanAllowance, error) {
			return map[string]client.StoragePlanAllowance{"private": {DiskGB: &capacity}}, nil
		}}
	old := m.loadStoragePlans()
	m.tenant = "b"
	m.tenantGeneration++
	current := m.loadStoragePlans()
	m.handleStoragePlansSnapshot(requireMessage[storagePlansSnapshot](t, old()))
	if m.storageRows[0].PlanDiskGB != nil {
		t.Fatal("old tenant's plan was applied to the current tenant")
	}
	m.handleStoragePlansSnapshot(requireMessage[storagePlansSnapshot](t, current()))
	if m.storageRows[0].PlanDiskGB == nil || *m.storageRows[0].PlanDiskGB != capacity {
		t.Fatal("current plan allowance was not applied")
	}
}

func TestStorageDetailsFormatUsageKeysAndTime(t *testing.T) {
	t.Parallel()
	m := &browser{storageKind: storageViewBuckets, storageSelectedID: "s1", storageRows: []client.Storage{{
		ID: "s1", Name: "Tehran", Buckets: []client.Bucket{{
			ID: "b1", Name: "media", Size: "1073741824", CreateTime: "2026-11-01T10:00:00Z",
		}}, Keys: []client.StorageKey{
			{Name: "reader", Actions: []string{"Read", "List"}, Buckets: []string{"media"}},
			{Name: "other-key", Actions: []string{"Write"}, Buckets: []string{"other"}},
		},
	}}}
	root := ansi.Strip(strings.Join(m.storageDetailLines(30), "\n"))
	for _, value := range []string{"DATACENTER", "1.0 GiB / —", "reader", "RL", "media", "other-key"} {
		if !strings.Contains(root, value) {
			t.Fatalf("datacenter details missing %q: %q", value, root)
		}
	}
	if strings.Contains(strings.ToLower(root), "read only") {
		t.Fatalf("datacenter details contain read-only hint: %q", root)
	}
	m.storageSelectedChild = "b1"
	bucket := ansi.Strip(strings.Join(m.storageDetailLines(30), "\n"))
	if !strings.Contains(bucket, "1.0 GiB") || strings.Contains(bucket, "2026-11-01T10:00:00Z") || !strings.Contains(bucket, "2026") {
		t.Fatalf("bucket size or time was not formatted: %q", bucket)
	}
	if !strings.Contains(bucket, storageIconKey+" reader") || !strings.Contains(bucket, "  Access   RL") ||
		strings.Contains(bucket, "other-key") {
		t.Fatalf("bucket key access was not filtered correctly: %q", bucket)
	}
	if got := storageUsage(client.Storage{Buckets: []client.Bucket{{Size: "invalid"}}}); got != "— / —" {
		t.Fatalf("unknown usage was reported as %q", got)
	}
	if got := storageTreeSummary(m.storageRows[0]); got != "1.0 GiB · 1 bucket" {
		t.Fatalf("unexpected datacenter usage and bucket count: %q", got)
	}
	if got := storageTreeSummary(client.Storage{}); got != "0 B · 0 buckets" {
		t.Fatalf("unexpected empty datacenter summary: %q", got)
	}
	allowance := 100.0
	if got := storageUsage(client.Storage{PlanDiskGB: &allowance}); got != "0 B / 100 GB plan" {
		t.Fatalf("plan allowance missing from usage: %q", got)
	}
}

func TestStoragePermissionInputAndVisibilityGlyphs(t *testing.T) {
	t.Parallel()
	for _, input := range []string{"RWL", "r,w,l", "Read,Write,List"} {
		actions, ok := parseStoragePermissions(input)
		if !ok || storagePermissions(actions) != "RWL" {
			t.Fatalf("permission input %q produced %v, valid %v", input, actions, ok)
		}
	}
	if _, ok := parseStoragePermissions("RX"); ok {
		t.Fatal("accepted an unknown permission")
	}
	m := &browser{storageKind: storageViewBuckets, storageRows: []client.Storage{{Buckets: []client.Bucket{
		{Name: "private", Size: "0"}, {Name: "public", Size: "0", IsPublic: true},
	}}}}
	private := ansi.Strip(m.storageTreeLine(storageTreeItem{storage: 0, child: 0}, 40))
	public := ansi.Strip(m.storageTreeLine(storageTreeItem{storage: 0, child: 1}, 40))
	if !strings.Contains(private, "private  0 B") || !strings.Contains(public, "public  0 B") {
		t.Fatalf("incorrect bucket rows: %q, %q", private, public)
	}
}

func TestStorageRefreshDoesNotReplaceRemovedSelection(t *testing.T) {
	t.Parallel()
	m := &browser{tenant: "example", tenantGeneration: 9, storageSelectedID: "removed"}
	first := requestMeta{tenant: "example", scopeGeneration: 9, requestID: 1}
	m.storageState.begin(first, m.requestTime())
	m.handleStorageSnapshot(storageSnapshot{meta: first, rows: []client.Storage{{ID: "other", Name: "Other"}}})
	if m.storageSelectedID != "" || !m.storageSelectionLost {
		t.Fatal("removed storage selection was silently replaced")
	}
	second := requestMeta{tenant: "example", scopeGeneration: 9, requestID: 2}
	m.storageState.begin(second, m.requestTime())
	m.handleStorageSnapshot(storageSnapshot{meta: second, rows: []client.Storage{{ID: "other", Name: "Other"}}})
	if m.storageSelectedID != "" {
		t.Fatal("a later refresh silently selected a replacement storage")
	}
}

func TestStorageTreeNavigationFollowsVisibleRows(t *testing.T) {
	t.Parallel()
	m := &browser{page: pageStorage, storageKind: storageViewBuckets, storageSelectedID: "s1", storageExpanded: true,
		storageRows: []client.Storage{
			{ID: "s1", Buckets: []client.Bucket{{ID: "a"}, {ID: "b"}}},
			{ID: "s2", Buckets: []client.Bucket{{ID: "c"}}},
		},
	}
	press := func(code rune) { m.handleStorageKey(tea.KeyPressMsg{Code: code, Text: string(code)}) }
	press('j')
	if m.storageSelectedChild != "a" {
		t.Fatalf("first child: %q", m.storageSelectedChild)
	}
	press('j')
	if m.storageSelectedChild != "b" {
		t.Fatalf("second child: %q", m.storageSelectedChild)
	}
	press('k')
	if m.storageSelectedChild != "a" {
		t.Fatalf("previous child: %q", m.storageSelectedChild)
	}
	press('h')
	if m.storageSelectedChild != "" || !m.storageExpanded {
		t.Fatal("left from child should select the parent")
	}
	press('h')
	if m.storageExpanded {
		t.Fatal("left from root should collapse it")
	}
	press('j')
	if m.storageSelectedID != "s2" || m.storageSelectedChild != "" || m.storageExpanded {
		t.Fatal("down on a collapsed root should select the next root")
	}
	press('l')
	press('j')
	if m.storageSelectedChild != "c" {
		t.Fatal("expanded root should allow selecting its child")
	}
}

func TestStorageTrafficRejectsLateSelectionResults(t *testing.T) {
	t.Parallel()
	m := &browser{page: pageStorage, tenant: "demo", tenantGeneration: 1, storageSelectedID: "a",
		getStorageTraffic: func(_ context.Context, _, storageID string) ([]client.StorageTrafficSample, error) {
			return []client.StorageTrafficSample{{Domain: storageID, BytesPerSecond: 1024, ObservedAt: time.Unix(10, 0)}}, nil
		}}
	first := m.loadStorageTraffic(false)
	m.storageSelectedID = "b"
	second := m.loadStorageTraffic(false)
	m.handleStorageTrafficSnapshot(requireMessage[storageTrafficSnapshot](t, first()))
	if len(m.storageTraffic) != 0 {
		t.Fatal("traffic for the previous storage appeared after selection changed")
	}
	m.handleStorageTrafficSnapshot(requireMessage[storageTrafficSnapshot](t, second()))
	if len(m.storageTraffic) != 1 || m.storageTraffic[0].Domain != "b" {
		t.Fatal("traffic for the selected storage was not accepted")
	}
}

func TestStorageOTPRejectionRequiresNewInput(t *testing.T) {
	t.Parallel()
	if !storageActionRequiresOTP(storageOpKeyRotate) || !storageOTPRejected(errTestStorageOTPRejected) {
		t.Fatal("OTP rejection was not identified for an OTP-protected write")
	}
	if storageActionRequiresOTP(storageOpBucketCreate) || storageOTPRejected(errTestStorageGeneric) {
		t.Fatal("non-OTP failures were classified as OTP rejection")
	}
}

func TestStorageWriteTargetCannotChangeAfterReview(t *testing.T) {
	t.Parallel()
	m := &browser{
		storageKind: storageViewKeys, storageSelectedID: "s1", storageSelectedChild: "key",
		storagePendingAction: storageOpKeyGrant,
		storageRows: []client.Storage{{ID: "s1", Keys: []client.StorageKey{{
			Name: "key", Actions: []string{client.StorageRead}, Buckets: []string{"media"},
		}}}},
	}
	m.captureStorageTarget()
	if !m.storageTargetMatches() {
		t.Fatal("reviewed target should match initially")
	}
	m.storageRows[0].Keys[0].Actions = append(m.storageRows[0].Keys[0].Actions, client.StorageWrite)
	if m.storageTargetMatches() {
		t.Fatal("changed grants must require a new review")
	}
	m.storageRows[0].Keys[0].Actions = []string{client.StorageRead}
	m.storageSelectedID = "different-storage"
	if m.storageTargetMatches() {
		t.Fatal("a different storage must not inherit confirmation")
	}
}

func TestStorageMissingCredentialsRemainsAnUnknownOutcome(t *testing.T) {
	t.Parallel()
	m := &browser{page: pageStorage, tenant: "test"}
	m.handleStorageWriteResult(storageWriteResult{tenant: "test", action: storageOpKeyRotate})
	if m.storageVerifyAction != "" || !strings.Contains(m.storageNotice, "no credentials were returned") {
		t.Fatalf("missing one-time credentials were reported as success: %q", m.storageNotice)
	}
	partial := &browser{page: pageStorage, tenant: "test"}
	partial.handleStorageWriteResult(storageWriteResult{tenant: "test", action: storageOpKeyRotate,
		secret: &client.StorageKeySecret{SecretKey: "partial-value"}})
	if partial.storageSecret == nil || partial.storageVerifyAction != "" || !strings.Contains(partial.storageNotice, "incomplete") {
		t.Fatalf("partial credentials were not preserved and marked incomplete: %q", partial.storageNotice)
	}
}

func TestStoragePaneFitsNarrowWidths(t *testing.T) {
	t.Parallel()
	for _, width := range []int{80, 48, 24} {
		m := &browser{width: width, tenant: "example", storageKind: storageViewBuckets,
			storageState: requestState{status: requestReady}, storageSelectedID: "s1", storageExpanded: true,
			storageRows: []client.Storage{{ID: "s1", Name: "A long storage label", Buckets: []client.Bucket{{
				ID: "b1", Name: "a-long-bucket-name", Status: "active",
			}}}},
		}
		lines := m.storagePaneLines(8)
		if len(lines) > 8 {
			t.Fatalf("width %d produced too many pane lines: %d", width, len(lines))
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > width {
				t.Fatalf("width %d produced an over-wide row: %q", width, line)
			}
		}
	}
}

func TestStorageWritesRequireOptInAndFreshState(t *testing.T) {
	t.Parallel()
	ready := requestState{status: requestReady}
	if (&browser{page: pageStorage, storageState: ready}).storageWritesReady() {
		t.Fatal("live read-only session enabled storage writes")
	}
	if !(&browser{page: pageStorage, allowWrites: true, storageState: ready}).storageWritesReady() {
		t.Fatal("opted-in ready session disabled storage writes")
	}
	if (&browser{page: pageStorage, allowWrites: true, storageState: requestState{status: requestStale}}).storageWritesReady() {
		t.Fatal("stale storage data enabled writes")
	}
	blocked := gateWriteCallbacks(Options{CreateStorageBucket: func(
		context.Context, string, string, string,
	) (string, error) {
		return "", nil
	}})
	if _, err := blocked.CreateStorageBucket(context.Background(), "tenant", "storage", "bucket"); err == nil {
		t.Fatal("write callback escaped launch-time gate")
	}
}

func TestMockStorageMutationDoesNotChangeFixture(t *testing.T) {
	t.Parallel()
	fixture := []client.Storage{{
		ID: "s1", Name: "Mock", Buckets: []client.Bucket{{ID: "b1", Name: "media"}, {ID: "b2", Name: "logs"}},
		Keys: []client.StorageKey{{Name: "reader", Actions: []string{client.StorageRead}, Buckets: []string{"media"}}},
	}}
	stateCopy := cloneMockStorages(fixture)
	m := &browser{storageRows: stateCopy}
	if err := m.applyMockStorageWrite("visibility", "s1", "", "b1", nil, nil); err != nil {
		t.Fatal(err)
	}
	if fixture[0].Buckets[0].IsPublic {
		t.Fatal("fixture changed through mock storage mutation")
	}
	if !stateCopy[0].Buckets[0].IsPublic {
		t.Fatal("mock storage action did not update the in-memory view")
	}
	if err := m.applyMockStorageWrite(storageOpKeyGrant, "s1", "", "reader",
		[]string{client.StorageRead, client.StorageWrite}, []string{"media", "logs"}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(m.storageRows[0].Keys[0].Buckets, ","); got != "media,logs" {
		t.Fatalf("mock grants duplicated a bucket or removed an existing grant: %q", got)
	}
	if got := strings.Join(fixture[0].Keys[0].Buckets, ","); got != "media" {
		t.Fatalf("mock grant changed fixture state: %q", got)
	}
}

func TestStorageKeyFormSelectsGrantsAndCreatesMockKey(t *testing.T) {
	t.Parallel()
	m := &browser{page: pageStorage, mode: "MOCK", tenant: "test", storageKind: storageViewKeys,
		storageState: requestState{status: requestReady}, storageSelectedID: "s1",
		storageRows: []client.Storage{{ID: "s1", Name: "Example", Buckets: []client.Bucket{{ID: "b1", Name: "media"}, {ID: "b2", Name: "logs"}}}}}
	m.handleStorageKey(tea.KeyPressMsg{Code: 'K', Text: "K"})
	if m.storageDialog != storageDialogKeyForm {
		t.Fatal("key form did not open")
	}
	for _, c := range "reader" {
		m.handleStorageDialogKey(tea.KeyPressMsg{Code: c, Text: string(c)})
	}
	m.storageDraft.field = 1
	m.handleStorageDialogKey(tea.KeyPressMsg{Code: ' ', Text: " "})
	m.storageDraft.field = 3
	m.handleStorageDialogKey(tea.KeyPressMsg{Code: ' ', Text: " "})
	m.storageDraft.field = 5
	m.handleStorageDialogKey(tea.KeyPressMsg{Code: ' ', Text: " "})
	if m.storageDraft.permissions != "RL" || m.storageDraft.buckets != "logs" {
		t.Fatalf("unexpected grants %q %q", m.storageDraft.permissions, m.storageDraft.buckets)
	}
	m.handleStorageDialogKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.storageDialog != storageDialogWriteConfirm || m.storageDraft.name != "reader" {
		t.Fatalf("form did not preserve review: %#v", m.storageDialog)
	}
	m.advanceStorageDialog()
	if m.storageDialog != storageDialogWriteConfirm || m.storageNotice != "Enter a 6-digit authenticator code" {
		t.Fatal("key creation must require OTP on review")
	}
	m.storageDraft.otp = "123456"
	m.advanceStorageDialog()
	if m.storageSecret == nil || len(m.storageRows[0].Keys) != 1 {
		t.Fatal("key creation failed")
	}
	k := m.storageRows[0].Keys[0]
	if k.Name != "reader" || strings.Join(k.Buckets, ",") != "logs" || storagePermissions(k.Actions) != "RL" {
		t.Fatalf("wrong key grants: %#v", k)
	}
}

func TestStorageKeyFormAlignmentAndReviewBuckets(t *testing.T) {
	t.Parallel()
	m := &browser{storageDialog: storageDialogKeyForm, storageSelectedID: "s",
		storageRows: []client.Storage{{ID: "s", Buckets: []client.Bucket{{Name: "one"}, {Name: "two"}}}}}
	form := ansi.Strip(strings.Join(m.storageDialogLines(), "\n"))
	for _, want := range []string{"Read   (R)", "Write  (W)", "List   (L)"} {
		if !strings.Contains(form, want) {
			t.Errorf("form missing aligned permission %q: %q", want, form)
		}
	}
	if strings.Contains(form, "Space select") {
		t.Fatal("form contains footer shortcuts")
	}
	m.storageDialog = storageDialogWriteConfirm
	m.storagePendingAction = storageOpKeyCreate
	m.storageDraft.buckets = "one,two"
	review := ansi.Strip(strings.Join(m.storageDialogLines(), "\n"))
	if !strings.Contains(review, "BUCKETS\n  • one\n  • two") {
		t.Fatalf("buckets not listed separately: %q", review)
	}
	if strings.Contains(review, "Enter continue") {
		t.Fatal("review contains footer shortcuts")
	}
}

func TestStorageBucketCreationAndVisibilityThroughKeyDispatch(t *testing.T) {
	t.Parallel()
	m := &browser{page: pageStorage, width: 120, height: 30, mode: "MOCK", tenant: "example", storageKind: storageViewBuckets,
		storageState: requestState{status: requestReady}, storageSelectedID: "s", storageRows: []client.Storage{{ID: "s", Name: "Example"}}}
	press := func(code rune) { m.handleKeyPress(tea.KeyPressMsg{Code: code, Text: string(code)}) }
	press('+')
	if m.storageDialog != storageDialogBucketName {
		t.Fatal("bucket modal did not open")
	}
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "CREATE BUCKET") {
		t.Fatalf("bucket modal missing: %q", view)
	}
	for _, ch := range "media" {
		press(ch)
	}
	m.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(m.storageRows[0].Buckets) != 1 || m.storageRows[0].Buckets[0].Name != "media" {
		t.Fatalf("bucket not created: %#v", m.storageRows[0].Buckets)
	}
	m.storageSelectedChild = "mock-media"
	press('p')
	if m.storageDialog != storageDialogVisibilityConfirm {
		t.Fatal("visibility modal did not open")
	}
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "CHANGE VISIBILITY") || !strings.Contains(view, "public") {
		t.Fatal("visibility target missing")
	}
	m.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.storageDialog != storageDialogVisibilityConfirm || m.storageNotice != "Enter a 6-digit authenticator code" {
		t.Fatal("visibility must require OTP in the modal")
	}
	for _, ch := range "123456" {
		press(ch)
	}
	m.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.storageRows[0].Buckets[0].IsPublic {
		t.Fatal("visibility did not change")
	}
}

func TestStorageKeyCreationThroughKeyDispatch(t *testing.T) {
	t.Parallel()
	m := &browser{page: pageStorage, width: 120, height: 30, mode: "MOCK", tenant: "example", storageKind: storageViewKeys,
		storageState: requestState{status: requestReady}, storageSelectedID: "s",
		storageRows: []client.Storage{{ID: "s", Buckets: []client.Bucket{{ID: "b", Name: "media"}}}}}
	press := func(code rune) { m.handleKeyPress(tea.KeyPressMsg{Code: code, Text: string(code)}) }
	key := func(code rune) { m.handleKeyPress(tea.KeyPressMsg{Code: code}) }
	press('K')
	for _, ch := range "reader" {
		press(ch)
	}
	key(tea.KeyTab)
	press(' ')
	key(tea.KeyTab)
	key(tea.KeyTab)
	key(tea.KeyTab)
	press(' ')
	key(tea.KeyEnter)
	if m.storageDialog != storageDialogWriteConfirm {
		t.Fatalf("key review did not open: %s %s", m.storageDialog, m.storageNotice)
	}
	key(tea.KeyEnter)
	if m.storageDialog != storageDialogWriteConfirm || m.storageNotice != "Enter a 6-digit authenticator code" {
		t.Fatal("review must require a TOTP code")
	}
	for _, ch := range "123456" {
		press(ch)
	}
	key(tea.KeyEnter)
	if m.storageSecret == nil || len(m.storageRows[0].Keys) != 1 {
		t.Fatalf("key creation failed: %s", m.storageNotice)
	}
}

func TestStorageCreationStatusAppearsOnPaneBorder(t *testing.T) {
	t.Parallel()
	for _, width := range []int{80, 140} {
		for _, detail := range []bool{false, true} {
			m := &browser{page: pageStorage, width: width, height: 28, detail: detail, storageKind: storageViewBuckets,
				storageNotice: "Creating bucket…", storageBusy: true, storageRows: []client.Storage{{ID: "s", Name: "Example"}}, storageSelectedID: "s"}
			view := ansi.Strip(m.View().Content)
			found := false
			for line := range strings.SplitSeq(view, "\n") {
				if strings.Contains(line, "╰") && strings.Contains(line, "Creating bucket") {
					found = true
				}
			}
			if !found {
				t.Errorf("width %d detail %t: status missing from bottom border: %q", width, detail, view)
			}
		}
	}
}

func TestStorageKeyCreationSendsEnteredTOTP(t *testing.T) {
	t.Parallel()
	var called bool
	m := &browser{page: pageStorage, mode: "LIVE", ctx: context.Background(), allowWrites: true, tenant: "test", storageKind: storageViewKeys,
		storageState: requestState{status: requestReady}, storageSelectedID: "s", storagePendingStorage: "s",
		storagePendingAction: storageOpKeyCreate, storageDialog: storageDialogWriteConfirm,
		storageDraft: storageActionDraft{name: "reader", permissions: "RL", buckets: "media"},
		storageRows:  []client.Storage{{ID: "s", Buckets: []client.Bucket{{ID: "bucket-id", Name: "media"}}}}}
	m.createStorageKey = func(
		_ context.Context, tenant, name string, actions, buckets []string, otp string,
	) (*client.StorageKeySecret, error) {
		called = true
		if tenant != "test" || name != "reader" || strings.Join(actions, ",") != "Read,List" ||
			strings.Join(buckets, ",") != "bucket-id" || otp != "123456" {
			t.Error("creation received incorrect target, grants, or two-factor code")
		}
		return &client.StorageKeySecret{AccessKey: "access", SecretKey: "secret"}, nil
	}
	m.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.storageDialog != storageDialogWriteConfirm || m.storageNotice != "Enter a 6-digit authenticator code" {
		t.Fatal("review did not request two-factor code")
	}
	for _, ch := range "123456" {
		m.handleKeyPress(tea.KeyPressMsg{Code: ch, Text: string(ch)})
	}
	_, cmd := m.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("entering the code did not submit")
	}
	result, ok := cmd().(storageWriteResult)
	if !ok || result.err != nil || !called {
		t.Fatal("key creation callback was not successful")
	}
}
