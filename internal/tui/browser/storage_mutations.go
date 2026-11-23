package browser

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/rahacloud/darkubectl/internal/client"
)

//nolint:gocognit,gocyclo,cyclop // Each operation has a distinct read-back outcome to report.
func (m *browser) storageVerificationMessage(rows []client.Storage) string {
	var storage *client.Storage
	for i := range rows {
		if rows[i].ID == m.storageVerifyStorage {
			storage = &rows[i]
			break
		}
	}
	if storage == nil {
		return "Change submitted; storage could not be verified"
	}
	findBucket := func() *client.Bucket {
		for i := range storage.Buckets {
			if storage.Buckets[i].ID == m.storageVerifyResource || storage.Buckets[i].Name == m.storageVerifyName {
				return &storage.Buckets[i]
			}
		}
		return nil
	}
	findKey := func() *client.StorageKey {
		for i := range storage.Keys {
			if storage.Keys[i].Name == m.storageVerifyResource || storage.Keys[i].Name == m.storageVerifyName {
				return &storage.Keys[i]
			}
		}
		return nil
	}
	switch m.storageVerifyAction {
	case storageOpBucketCreate:
		if b := findBucket(); b != nil {
			return "Bucket created; status: " + safeText(b.Status)
		}
		return "Bucket creation submitted; read-back did not find it"
	case storageOpBucketDelete:
		if findBucket() == nil {
			return "Bucket deletion verified"
		}
		return "Bucket still appears in read-back"
	case storageOpVisibility:
		if b := findBucket(); b != nil && b.IsPublic == m.storageVerifyPublic {
			return "Bucket visibility verified"
		}
		return "Visibility change was not verified"
	case storageOpKeyCreate:
		if k := findKey(); k != nil && sameStorageSet(k.Actions, m.storageVerifyActions) && sameStorageSet(k.Buckets, m.storageVerifyBuckets) {
			return "Key grants verified; save the one-time credentials"
		}
		return "Key created; grant read-back needs review"
	case storageOpKeyGrant:
		if k := findKey(); k != nil && sameStorageSet(k.Actions, m.storageVerifyActions) && sameStorageSet(k.Buckets, m.storageVerifyBuckets) {
			return "Key grants verified"
		}
		return "Grant update was not verified"
	case storageOpKeyDelete:
		if findKey() == nil {
			return "Key revocation verified"
		}
		return "Key still appears in read-back"
	case storageOpKeyRotate:
		if findKey() != nil {
			return "Key rotated; old pair is revoked; save the one-time credentials"
		}
		return "Rotation submitted; key could not be verified"
	}
	return "Storage refreshed"
}

func (m *browser) storageBucketAwaitingReady(rows []client.Storage) bool {
	for _, storage := range rows {
		if storage.ID != m.storageVerifyStorage {
			continue
		}
		for _, bucket := range storage.Buckets {
			if bucket.ID == m.storageVerifyResource || bucket.Name == m.storageVerifyName {
				return bucket.Status != storageBucketActive && bucket.Status != storageBucketFailed && bucket.Status != storageBucketError
			}
		}
	}
	return true
}

func (m *browser) storageVerificationOutcome(rows []client.Storage) storageOutcome {
	if m.storageVerifyAction == storageOpBucketCreate {
		for _, storage := range rows {
			if storage.ID != m.storageVerifyStorage {
				continue
			}
			for _, bucket := range storage.Buckets {
				if bucket.Name == m.storageVerifyName && (bucket.Status == storageBucketFailed || bucket.Status == storageBucketError) {
					return storageOutcomeFailed
				}
			}
		}
	}
	text := strings.ToLower(m.storageVerificationMessage(rows))
	if strings.Contains(text, "not verified") || strings.Contains(text, "could not") || strings.Contains(text, "needs review") ||
		strings.Contains(text, "still appears") || strings.Contains(text, "did not find") {
		return storageOutcomeUnknown
	}
	return storageOutcomeVerified
}

func sameStorageSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, v := range a {
		if !containsStorageValue(b, v) {
			return false
		}
	}
	return true
}

func (m *browser) storageWritesReady() bool {
	return (m.actionIsMock() || m.allowWrites) && !m.storageBusy && m.storageVerifyAction == "" && !m.storageState.active() &&
		(m.storageState.status == requestReady || m.storageState.status == requestEmpty) && m.storageState.err == nil
}

func (m *browser) captureStorageTarget() {
	m.clearStorageInputs()
	m.storageNotice = ""
	m.storageFeedback = ""
	m.storageOutcome = ""
	m.storageDetailScroll = 0
	m.storagePendingStorage = m.storageSelectedID
	m.storagePendingResource = ""
	m.storagePendingActions = nil
	m.storagePendingBuckets = nil
	if bucket := m.selectedBucket(); bucket != nil &&
		(m.storagePendingAction == storageOpBucketDelete || m.storagePendingAction == storageOpVisibility) {
		m.storagePendingResource = bucket.ID
		m.storagePendingPublic = bucket.IsPublic
	}
	if key := m.selectedKey(); key != nil && (m.storagePendingAction == storageOpKeyGrant ||
		m.storagePendingAction == storageOpKeyDelete || m.storagePendingAction == storageOpKeyRotate) {
		m.storagePendingResource = key.Name
		m.storagePendingActions = append([]string(nil), key.Actions...)
		m.storagePendingBuckets = append([]string(nil), key.Buckets...)
	}
}

func (m *browser) storageTargetMatches() bool {
	if m.storagePendingStorage == "" || m.storagePendingStorage != m.storageSelectedID || m.selectedStorage() == nil {
		return false
	}
	switch m.storagePendingAction {
	case storageOpBucketDelete, storageOpVisibility:
		bucket := m.selectedBucket()
		return bucket != nil && bucket.ID == m.storagePendingResource && bucket.IsPublic == m.storagePendingPublic
	case storageOpKeyGrant, storageOpKeyDelete, storageOpKeyRotate:
		key := m.selectedKey()
		return key != nil && key.Name == m.storagePendingResource &&
			sameStorageSet(key.Actions, m.storagePendingActions) && sameStorageSet(key.Buckets, m.storagePendingBuckets)
	default:
		return true
	}
}

//nolint:gocognit,gocyclo,cyclop,nestif,funlen,maintidx // One path keeps OTP and read-back behavior consistent.
func (m *browser) submitStorageWrite() tea.Cmd {
	if m.storageBusy {
		return nil
	}
	if !m.actionIsMock() && !m.allowWrites {
		m.storageNotice = errWriteActionsAreDisabledRelaunchWithAllow.Error()
		return nil
	}
	if !m.storageWritesReady() {
		m.storageNotice = "Wait for a successful storage refresh before submitting"
		return nil
	}
	if !m.storageTargetMatches() {
		m.storageDialog = ""
		m.storageDraft.otp = ""
		m.storagePendingAction = ""
		m.storageNotice = "Storage target or permissions changed; review the action again"
		return nil
	}
	s := m.selectedStorage()
	if s == nil {
		m.storageDialog = ""
		return nil
	}
	if storageActionRequiresOTP(m.storagePendingAction) && !m.validateStorageOTP() {
		return nil
	}
	action, tenant, storageID, otp := m.storagePendingAction, m.tenant, s.ID, m.storageDraft.otp
	name := strings.TrimSpace(m.storageDraft.text)
	if action == storageOpKeyCreate || action == storageOpBucketCreate {
		name = strings.TrimSpace(m.storageDraft.name)
	}
	resourceID := ""
	switch action {
	case storageOpBucketDelete, storageOpVisibility:
		if b := m.selectedBucket(); b != nil {
			resourceID = b.ID
		}
	case storageOpKeyGrant, storageOpKeyRotate, storageOpKeyDelete:
		if k := m.selectedKey(); k != nil {
			resourceID = k.Name
		}
	}
	if action == storageOpKeyRotate {
		name = resourceID
	}
	var actions []string
	validActions := true
	if action == storageOpKeyCreate || action == storageOpKeyGrant {
		actions, validActions = parseStoragePermissions(m.storageDraft.permissions)
	}
	if !validActions {
		m.storageNotice = "Access must use R, W or L"
		return nil
	}
	var bucketNames []string
	if action == storageOpKeyCreate || action == storageOpKeyGrant {
		bucketNames = splitStorageNames(m.storageDraft.buckets)
	}
	bucketIDs := make([]string, 0, len(bucketNames))
	for _, bucketName := range bucketNames {
		found := false
		for _, b := range s.Buckets {
			if b.Name == bucketName {
				bucketIDs = append(bucketIDs, b.ID)
				found = true
				break
			}
		}
		if !found {
			m.storageNotice = "Every bucket must belong to this storage"
			return nil
		}
	}
	if action == storageOpKeyCreate && (name == "" || len(actions) == 0 || len(bucketIDs) == 0) {
		m.storageNotice = "Key needs a name, actions and buckets"
		return nil
	}
	var grantBaselineActions, grantBaselineBuckets []string
	if action == storageOpKeyGrant {
		key := m.selectedKey()
		if key == nil {
			return nil
		}
		grantBaselineActions = append([]string(nil), key.Actions...)
		grantBaselineBuckets = append([]string(nil), key.Buckets...)
		for _, old := range key.Actions {
			if !containsStorageValue(actions, old) {
				actions = append(actions, old)
			}
		}
		for _, old := range key.Buckets {
			found := false
			for _, b := range s.Buckets {
				if b.Name == old {
					if !containsStorageValue(bucketIDs, b.ID) {
						bucketIDs = append(bucketIDs, b.ID)
					}
					found = true
					break
				}
			}
			if !found {
				m.storageNotice = "Current key bucket is no longer present"
				return nil
			}
		}
	}
	endpoint := s.Endpoint()
	public := false
	if b := m.selectedBucket(); b != nil {
		public = !b.IsPublic
	}
	resourceName := name
	if action == storageOpBucketDelete || action == storageOpVisibility {
		if b := m.selectedBucket(); b != nil {
			resourceName = b.Name
		}
	}
	if action == storageOpKeyDelete || action == storageOpKeyRotate || action == storageOpKeyGrant {
		if k := m.selectedKey(); k != nil {
			resourceName = k.Name
		}
	}
	verifyBuckets := append([]string(nil), bucketNames...)
	if action == storageOpKeyGrant {
		key := m.selectedKey()
		if key != nil {
			for _, old := range key.Buckets {
				if !containsStorageValue(verifyBuckets, old) {
					verifyBuckets = append(verifyBuckets, old)
				}
			}
		}
	}
	if m.actionIsMock() {
		if err := m.applyMockStorageWrite(action, storageID, name, resourceID, actions, bucketNames); err != nil {
			m.storageNotice = safeText(err.Error())
			return nil
		}
		if m.saveMockStorages != nil {
			m.saveMockStorages(tenant, m.storageRows)
		}
		m.storageOutcome = storageOutcomeVerified
		if action == storageOpKeyCreate || action == storageOpKeyRotate {
			m.storageSecret = &client.StorageKeySecret{
				Name: name, Actions: actions,
				AccessKey: "MOCK-ACCESS-KEY", SecretKey: "MOCK-SECRET-KEY",
			}
			m.storageSecretEndpoint = endpoint
			m.storageSecretVisible = false
			verb := "created"
			if action == storageOpKeyRotate {
				verb = "rotated"
			}
			m.storageNotice = "Mock key " + verb + "; credentials are synthetic and shown once"
		} else {
			m.storageNotice = "Mock storage updated in memory"
		}
		m.storageDialog = ""
		m.storagePendingAction = ""
		if m.storageSelectedChild != "" {
			m.syncStorageChildSelection(m.selectedStorage(), m.storageSelectedChild)
		}
		m.clearStorageInputs()
		return nil
	}
	m.storageBusy = true
	m.storageOutcome = storageOutcomePending
	m.storageFeedback = ""
	m.storageDialog = ""
	m.storageDraft.otp = ""
	switch action {
	case storageOpBucketCreate:
		m.storageNotice = "Creating bucket…"
	case storageOpKeyCreate:
		m.storageNotice = "Creating key…"
	default:
		m.storageNotice = "Submitting storage operation…"
	}
	return func() tea.Msg {
		result := storageWriteResult{
			tenant: tenant, storageID: storageID, resource: resourceID, resourceName: resourceName,
			action: action, endpoint: endpoint, public: public,
			verifyActions: append([]string(nil), actions...), verifyBuckets: verifyBuckets,
		}
		switch action {
		case storageOpBucketCreate:
			result.resource, result.err = m.createStorageBucket(m.ctx, tenant, storageID, name)
		case storageOpBucketDelete:
			result.err = m.deleteStorageBucket(m.ctx, tenant, resourceID, otp)
		case storageOpVisibility:
			result.err = m.setStorageBucketPublic(m.ctx, tenant, resourceID, public, otp)
		case storageOpKeyCreate:
			result.secret, result.err = m.createStorageKey(m.ctx, tenant, name, actions, bucketIDs, otp)
		case storageOpKeyGrant:
			fresh, readErr := m.listStorages(m.ctx, tenant)
			if readErr != nil {
				result.err = fmt.Errorf("could not recheck key grants before update: %w", readErr)
				break
			}
			unchanged := false
			for _, currentStorage := range fresh {
				if currentStorage.ID != storageID {
					continue
				}
				for _, currentKey := range currentStorage.Keys {
					if currentKey.Name == resourceID {
						unchanged = sameStorageSet(currentKey.Actions, grantBaselineActions) && sameStorageSet(currentKey.Buckets, grantBaselineBuckets)
					}
				}
			}
			if !unchanged {
				result.err = errStorageGrantChanged
				break
			}
			result.err = m.updateStorageKey(m.ctx, tenant, resourceID, actions, bucketIDs, otp)
		case storageOpKeyRotate:
			result.secret, result.err = m.rotateStorageKey(m.ctx, tenant, resourceID, otp)
		case storageOpKeyDelete:
			result.err = m.deleteStorageKey(m.ctx, tenant, resourceID, otp)
		default:
			result.err = errStorageUnsupportedOperation
		}
		return result
	}
}

func (m *browser) handleStorageWriteResult(msg storageWriteResult) (tea.Model, tea.Cmd, bool) {
	m.storageBusy = false
	if msg.tenant != m.tenant || msg.tenant == "" {
		return m, nil, true
	}
	if msg.err != nil {
		return m.handleStorageWriteError(msg)
	}
	m.storageVerifyID++
	m.storageVerifyAttempts = 0
	m.storageOutcome = storageOutcomeVerifying
	m.storageVerifyAction = msg.action
	m.storageVerifyStorage = msg.storageID
	m.storageVerifyResource = msg.resource
	m.storageVerifyName = msg.resourceName
	m.storageVerifyPublic = msg.public
	m.storageVerifyActions = msg.verifyActions
	m.storageVerifyBuckets = msg.verifyBuckets
	if msg.action == storageOpBucketCreate {
		m.storageNotice = "Bucket created; checking status…"
	}
	if msg.secret != nil {
		m.storageSecret = msg.secret
		m.storageSecretEndpoint = msg.endpoint
		m.storageSecretVisible = false
		if msg.secret.AccessKey == "" || msg.secret.SecretKey == "" {
			m.storageVerifyAction = ""
			m.storageOutcome = storageOutcomeUnknown
			m.storageNotice = "The response contained an incomplete credential pair; inspect the key before retrying"
		} else {
			m.storageNotice = "New key created; save its credentials now"
		}
	} else if msg.action == storageOpKeyCreate || msg.action == storageOpKeyRotate {
		m.storageVerifyAction = ""
		m.storageOutcome = storageOutcomeUnknown
		m.storageNotice = "The operation may have succeeded, but no credentials were returned; inspect the key before retrying"
	}
	m.storagePendingAction = ""
	m.clearStorageInputs()
	return m, m.loadStorages(true), true
}

func (m *browser) handleStorageWriteError(msg storageWriteResult) (tea.Model, tea.Cmd, bool) {
	m.storageNotice = "Storage operation failed; refreshed state before retry: " + safeText(msg.err.Error())
	m.storageOutcome = storageOutcomeRejected
	definite := storageWriteDefinitelyRejected(msg.err)
	if !definite {
		m.storageOutcome = storageOutcomeUnknown
		m.storageNotice = "The change may have succeeded; refresh before retrying: " + safeText(msg.err.Error())
	}
	if !definite && (msg.action == storageOpKeyCreate || msg.action == storageOpKeyRotate) {
		m.storageNotice = "Key credentials were not returned; the operation may have succeeded. " +
			"Refreshed before retry: " + safeText(msg.err.Error())
	}
	switch {
	case storageActionRequiresOTP(msg.action) && storageOTPRejected(msg.err):
		m.storagePendingAction = msg.action
		m.storageDialog = storageDialogOTP
		if msg.action == storageOpKeyCreate || msg.action == storageOpKeyGrant {
			m.storageDialog = storageDialogWriteConfirm
		}
		m.storageDraft.otp = ""
		m.storageNotice = "Two-factor code was rejected; enter a new code. The operation was not replayed."
	case definite && (msg.action == storageOpKeyCreate || msg.action == storageOpBucketCreate):
		m.storagePendingAction = msg.action
		m.storageDraft.otp = ""
		m.storageDraft.text = m.storageDraft.name
		m.storageDraft.field = 0
		m.storageDialog = storageDialogKeyForm
		if msg.action == storageOpBucketCreate {
			m.storageDialog = storageDialogBucketName
		}
	default:
		m.storagePendingAction = ""
	}
	return m, m.loadStorages(true), true
}

func storageWriteDefinitelyRejected(err error) bool {
	var apiErr *client.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode >= http.StatusBadRequest && apiErr.StatusCode < http.StatusInternalServerError {
		return true
	}
	return storageOTPRejected(err) || errors.Is(err, client.ErrBucketNameTaken) || errors.Is(err, errStorageGrantChanged) ||
		errors.Is(err, errWriteActionsAreDisabledRelaunchWithAllow)
}

func storageActionRequiresOTP(action string) bool {
	return action != storageOpBucketCreate && action != ""
}

func storageOTPRejected(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, client.ErrOTPRequired) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "2fa") && strings.Contains(message, "code")
}

//nolint:gocognit,gocyclo,cyclop // The mock mirrors supported operations against an isolated in-memory copy.
func (m *browser) applyMockStorageWrite(action, storageID, name, resource string, actions, bucketNames []string) error {
	for i := range m.storageRows {
		s := &m.storageRows[i]
		if s.ID != storageID {
			continue
		}
		switch action {
		case storageOpBucketCreate:
			for _, b := range s.Buckets {
				if b.Name == name {
					return errStorageBucketExists
				}
			}
			s.Buckets = append(s.Buckets, client.Bucket{ID: "mock-" + name, Name: name, Status: storageBucketActive,
				Size: "0", CreateTime: mockNow().Format(time.RFC3339)})
		case storageOpBucketDelete:
			for j, b := range s.Buckets {
				if b.ID == resource {
					s.Buckets = append(s.Buckets[:j], s.Buckets[j+1:]...)
					return nil
				}
			}
		case storageOpVisibility:
			for j := range s.Buckets {
				if s.Buckets[j].ID == resource {
					s.Buckets[j].IsPublic = !s.Buckets[j].IsPublic
					return nil
				}
			}
		case storageOpKeyCreate:
			s.Keys = append(s.Keys, client.StorageKey{Name: name, Actions: actions, Buckets: bucketNames})
		case storageOpKeyGrant:
			for j, k := range s.Keys {
				if k.Name == resource {
					s.Keys[j].Actions = actions
					for _, bucketName := range bucketNames {
						if !containsStorageValue(s.Keys[j].Buckets, bucketName) {
							s.Keys[j].Buckets = append(s.Keys[j].Buckets, bucketName)
						}
					}
					return nil
				}
			}
		case storageOpKeyDelete:
			for j, k := range s.Keys {
				if k.Name == resource {
					s.Keys = append(s.Keys[:j], s.Keys[j+1:]...)
					return nil
				}
			}
		case storageOpKeyRotate:
			return nil
		}
		return nil
	}
	return errStorageMockNotFound
}
