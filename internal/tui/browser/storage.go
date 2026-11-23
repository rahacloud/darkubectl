package browser

import (
	"errors"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/rahacloud/darkubectl/internal/client"
)

const (
	storageViewBuckets               = "buckets"
	storageBucketActive              = "active"
	storageBucketError               = "error"
	storageBucketFailed              = "failed"
	storageVisibilityPublic          = "public"
	storageViewKeys                  = "keys"
	storageOpBucketCreate            = "bucket-create"
	storageOpBucketDelete            = "bucket-delete"
	storageOpVisibility              = "visibility"
	storageOpKeyCreate               = "key-create"
	storageOpKeyGrant                = "key-grant"
	storageOpKeyDelete               = "key-delete"
	storageOpKeyRotate               = "key-rotate"
	storageDialogSavePath            = "save-path"
	storageDialogSaveReview          = "save-review"
	storageDialogKeyForm             = "key-form"
	storageDialogBucketName          = "bucket-name"
	storageDialogVisibilityConfirm   = "visibility-confirm"
	storageDialogBucketDeleteConfirm = "bucket-delete-confirm"
	storageDialogKeyDeleteConfirm    = "key-delete-confirm"
	storageDialogOTP                 = "otp"
	storageDialogKeyRotateConfirm    = "key-rotate-confirm"
	storageDialogWriteConfirm        = "write-confirm"
	storageIconDatacenter            = "󰆼"
	storageIconBucket                = "󱐕" // Nerd Font md-bucket
	storageIconKey                   = "󰌋"
	storageIconTraffic               = "󰓅"
)

type storageActionDraft struct {
	text         string
	name         string
	permissions  string
	buckets      string
	field        int
	otp          string
	exportPath   string
	exportFormat string
}

type storageOutcome string

const (
	storageOutcomePending   storageOutcome = "pending"
	storageOutcomeVerifying storageOutcome = "verifying"
	storageOutcomeVerified  storageOutcome = "verified"
	storageOutcomeRejected  storageOutcome = "rejected"
	storageOutcomeUnknown   storageOutcome = "unknown"
	storageOutcomeFailed    storageOutcome = "failed"
)

var (
	errStorageUnavailable          = errors.New("object storage is unavailable in this mode")
	errStorageGrantChanged         = errors.New("key grants changed since review; refresh and review the new grants")
	errStorageUnsupportedOperation = errors.New("unsupported object storage operation")
	errStorageBucketExists         = errors.New("bucket name already exists")
	errStorageMockNotFound         = errors.New("mock storage not found")
)

func (m *browser) resetStorageContext() {
	m.storageState.reset()
	m.storagePlansState.reset()
	m.storageTrafficState.reset()
	m.storageTrafficID = ""
	m.storageTraffic = nil
	m.storageNotice = ""
	m.storageFeedback = ""
	m.storageFeedbackID++
	m.storageVerifyID++
	m.storageVerifyAttempts = 0
	m.storageOutcome = ""
	m.storageRows = nil
	m.storageCursor = 0
	m.storageSelectedID = ""
	m.storageExpanded = false
	m.storageSelectionLost = false
	m.storageSelectedChild = ""
	m.storageDialog = ""
	m.storageSecret = nil
	m.storageSecretEndpoint = ""
	m.storageSecretVisible = false
	m.storagePendingAction = ""
	m.storagePendingStorage = ""
	m.storagePendingResource = ""
	m.storagePendingActions = nil
	m.storagePendingBuckets = nil
	m.storageDraft = storageActionDraft{}
	m.storageVerifyAction = ""
	m.storageVerifyStorage = ""
	m.storageVerifyResource = ""
	m.storageVerifyName = ""
	m.storageVerifyActions = nil
	m.storageVerifyBuckets = nil
	m.storageVerifyPublic = false
	m.storageDetailScroll = 0
}

const (
	storageVerificationLimit    = 15
	storageVerificationInterval = 2 * time.Second
	storageFeedbackDuration     = 3 * time.Second
	storageModalWidth           = 60
)

type storageVerificationTick struct {
	tenant         string
	generation, id uint64
}
type storageFeedbackTimeout struct{ id uint64 }

func (m *browser) storageFeedbackMessage(message string) tea.Cmd {
	m.storageFeedback = message
	m.storageFeedbackID++
	id := m.storageFeedbackID
	return tea.Tick(storageFeedbackDuration, func(time.Time) tea.Msg { return storageFeedbackTimeout{id: id} })
}

func (m *browser) storageVerificationTimer() tea.Cmd {
	tick := storageVerificationTick{tenant: m.tenant, generation: m.tenantGeneration, id: m.storageVerifyID}
	return tea.Tick(storageVerificationInterval, func(time.Time) tea.Msg { return tick })
}

type storageSnapshot struct {
	meta requestMeta
	rows []client.Storage
	err  error
}

type storageTrafficSnapshot struct {
	meta    requestMeta
	samples []client.StorageTrafficSample
	err     error
}

type storagePlansSnapshot struct {
	meta       requestMeta
	allowances map[string]client.StoragePlanAllowance
	err        error
}

func (m *browser) handleStorageDataResult(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case storageFeedbackTimeout:
		if msg.id == m.storageFeedbackID {
			m.storageFeedback = ""
		}
		return m, nil, true
	case storageVerificationTick:
		if msg.tenant != m.tenant || msg.generation != m.tenantGeneration || msg.id != m.storageVerifyID || m.storageVerifyAction == "" {
			return m, nil, true
		}
		if m.page != pageStorage {
			m.storageVerifyAction = ""
			m.storageOutcome = storageOutcomeUnknown
			m.storageNotice = "Change submitted; verification paused after leaving Storage"
			return m, nil, true
		}
		return m, m.loadStorages(true), true
	case storageSnapshot:
		valid := m.storageState.matches(msg.meta) && msg.meta.tenant == m.tenant && msg.meta.scopeGeneration == m.tenantGeneration
		model := m.handleStorageSnapshot(msg)
		if valid && msg.err == nil {
			if m.storageVerifyAction != "" {
				return model, m.storageVerificationTimer(), true
			}
			return model, tea.Batch(m.loadStorageTraffic(true), m.loadStoragePlans()), true
		}
		return asHandledModel(model)
	case storageTrafficSnapshot:
		return asHandledModel(m.handleStorageTrafficSnapshot(msg))
	case storagePlansSnapshot:
		return asHandledModel(m.handleStoragePlansSnapshot(msg))
	case storageWriteResult:
		return m.handleStorageWriteResult(msg)
	default:
		return nil, nil, false
	}
}

type storageWriteResult struct {
	tenant, storageID, resource, action string
	resourceName                        string
	secret                              *client.StorageKeySecret
	endpoint                            string
	public                              bool
	verifyActions, verifyBuckets        []string
	err                                 error
}

type storageTreeItem struct{ storage, child int }

func (m *browser) loadStorages(force bool) tea.Cmd {
	if m.page != pageStorage || (!force && (m.storageState.active() ||
		m.storageState.status == requestReady || m.storageState.status == requestEmpty)) {
		return nil
	}
	meta, ctx := m.startRequest(requestStorage, m.tenant, "", m.tenantGeneration)
	m.storageState.begin(meta, m.requestTime())
	if m.listStorages == nil {
		m.finishRequest(requestStorage, meta)
		m.storageState.unavailable(errStorageUnavailable)
		return nil
	}
	return func() tea.Msg {
		rows, err := m.listStorages(ctx, meta.tenant)
		return storageSnapshot{meta: meta, rows: rows, err: err}
	}
}

func (m *browser) loadStorageTraffic(force bool) tea.Cmd {
	if m.page != pageStorage || m.storageSelectedID == "" {
		return nil
	}
	if !force && m.storageTrafficID == m.storageSelectedID &&
		(m.storageTrafficState.active() || m.storageTrafficState.status != requestIdle) {
		return nil
	}
	m.cancelRequest(requestKey{kind: requestStorageTraffic})
	if m.storageTrafficID != m.storageSelectedID {
		m.storageTrafficState.reset()
		m.storageTraffic = nil
	}
	m.storageTrafficID = m.storageSelectedID
	if m.getStorageTraffic == nil {
		m.storageTrafficState.unavailable(errStorageUnavailable)
		return nil
	}
	meta, ctx := m.startRequest(requestStorageTraffic, m.tenant, m.storageSelectedID, m.tenantGeneration)
	m.storageTrafficState.begin(meta, m.requestTime())
	return func() tea.Msg {
		samples, err := m.getStorageTraffic(ctx, meta.tenant, meta.appID)
		return storageTrafficSnapshot{meta: meta, samples: samples, err: err}
	}
}

func (m *browser) loadStoragePlans() tea.Cmd {
	if m.page != pageStorage || m.getStoragePlans == nil {
		return nil
	}
	meta, ctx := m.startRequest(requestStoragePlans, m.tenant, "", m.tenantGeneration)
	m.storagePlansState.begin(meta, m.requestTime())
	return func() tea.Msg {
		allowances, err := m.getStoragePlans(ctx, meta.tenant)
		return storagePlansSnapshot{meta: meta, allowances: allowances, err: err}
	}
}

func (m *browser) handleStoragePlansSnapshot(msg storagePlansSnapshot) tea.Model {
	m.finishRequest(requestStoragePlans, msg.meta)
	if !m.storagePlansState.matches(msg.meta) || msg.meta.tenant != m.tenant || msg.meta.scopeGeneration != m.tenantGeneration {
		return m
	}
	if msg.err != nil {
		m.storagePlansState.fail(msg.err)
		return m
	}
	for i := range m.storageRows {
		allowance := msg.allowances[m.storageRows[i].ID]
		m.storageRows[i].PlanDiskGB = allowance.DiskGB
		m.storageRows[i].PlanTrafficGB = allowance.TrafficGB
	}
	m.storagePlansState.succeed(len(msg.allowances) == 0, m.requestTime())
	return m
}

func (m *browser) handleStorageTrafficSnapshot(msg storageTrafficSnapshot) tea.Model {
	m.finishRequest(requestStorageTraffic, msg.meta)
	if !m.storageTrafficState.matches(msg.meta) || msg.meta.tenant != m.tenant ||
		msg.meta.scopeGeneration != m.tenantGeneration || msg.meta.appID != m.storageSelectedID {
		return m
	}
	if msg.err != nil {
		m.storageTrafficState.fail(msg.err)
		return m
	}
	m.storageTraffic = msg.samples
	m.storageTrafficState.succeed(len(msg.samples) == 0, m.requestTime())
	return m
}

func (m *browser) handleStorageSnapshot(msg storageSnapshot) tea.Model {
	m.finishRequest(requestStorage, msg.meta)
	if !m.storageState.matches(msg.meta) || msg.meta.tenant != m.tenant || msg.meta.scopeGeneration != m.tenantGeneration {
		return m
	}
	if msg.err != nil {
		m.storageState.fail(msg.err)
		if m.storageVerifyAction != "" {
			m.storageOutcome = storageOutcomeUnknown
			m.storageNotice = "Change submitted; verification failed: " + safeText(msg.err.Error())
			m.storageVerifyAction = ""
		}
		return m
	}
	m.syncStorageRows(msg.rows)
	m.storageState.succeed(len(msg.rows) == 0, m.requestTime())
	m.updateStorageVerification(msg.rows)
	return m
}

func (m *browser) updateStorageVerification(rows []client.Storage) {
	if m.storageVerifyAction != "" {
		m.storageNotice = m.storageVerificationMessage(rows)
		if m.storageVerifyAction == storageOpBucketCreate && m.storageBucketAwaitingReady(rows) {
			m.storageVerifyAttempts++
			if m.storageVerifyAttempts < storageVerificationLimit {
				m.storageOutcome = storageOutcomeVerifying
				return
			}
			m.storageNotice = "Bucket creation submitted; readiness check timed out. Refresh to check again."
			m.storageOutcome = storageOutcomeUnknown
		} else {
			m.storageOutcome = m.storageVerificationOutcome(rows)
		}
		m.storageVerifyAction = ""
	}
}

func (m *browser) syncStorageRows(rows []client.Storage) {
	previousChild := m.storageSelectedChild
	for i := range rows {
		for _, old := range m.storageRows {
			if old.ID == rows[i].ID {
				if rows[i].PlanDiskGB == nil {
					rows[i].PlanDiskGB = old.PlanDiskGB
				}
				if rows[i].PlanTrafficGB == nil {
					rows[i].PlanTrafficGB = old.PlanTrafficGB
				}
			}
		}
	}
	m.storageRows = rows
	if m.storageSelectedID == "" && !m.storageSelectionLost && len(rows) > 0 {
		m.storageSelectedID = rows[0].ID
		m.storageExpanded = true
	}
	if m.storageSelectedID != "" {
		found := false
		for i := range rows {
			s := &rows[i]
			if s.ID == m.storageSelectedID {
				found = true
				m.storageCursor = i
				if previousChild != "" {
					m.syncStorageChildSelection(s, previousChild)
				}
				break
			}
		}
		if !found {
			m.storageSelectedID = ""
			m.storageCursor = 0
			m.storageSelectedChild = ""
			m.storageSelectionLost = true
		}
	}
}

func (m *browser) storagePaneStatus() string {
	if m.storageDialog == storageDialogSavePath && strings.TrimSpace(m.storageDraft.text) != "" {
		if _, _, err := storageExportPath(m.storageDraft.text, storageHome(), storageWorkingDir()); err != nil {
			return err.Error()
		}
	}
	if m.storageFeedback != "" {
		return m.storageFeedback
	}
	if m.storageBusy || m.storageOutcome == storageOutcomeVerifying {
		return m.storageNotice + " " + m.spinnerGlyph()
	}
	return m.storageNotice
}
