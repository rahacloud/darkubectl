package browser

import (
	"context"
	"time"

	"github.com/rahacloud/darkubectl/internal/client"
)

func (m *browser) requestTime() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

func (m *browser) startRequest(kind requestKind, tenant, appID string, scopeGeneration uint64) (requestMeta, context.Context) {
	if m.inFlight == nil {
		m.inFlight = make(map[requestKey]inFlightRequest)
	}
	key := requestKey{kind: kind, appID: appID}
	if active, ok := m.inFlight[key]; ok {
		active.cancel()
	}
	parent := m.ctx
	if parent == nil {
		parent = context.Background()
	}
	requestCtx, cancel := context.WithCancel(parent)
	m.nextRequestID++
	meta := requestMeta{tenant: tenant, appID: appID, scopeGeneration: scopeGeneration, requestID: m.nextRequestID}
	m.inFlight[key] = inFlightRequest{meta: meta, cancel: cancel}
	return meta, requestCtx
}

func (m *browser) finishRequest(kind requestKind, meta requestMeta) {
	key := requestKey{kind: kind, appID: meta.appID}
	if active, ok := m.inFlight[key]; ok && active.meta.requestID == meta.requestID {
		active.cancel()
		delete(m.inFlight, key)
	}
}

func (m *browser) cancelRequests(keepProfile bool) {
	for key, active := range m.inFlight {
		if keepProfile && (key.kind == requestProfile || key.kind == requestNotifications) {
			continue
		}
		active.cancel()
		delete(m.inFlight, key)
	}
}

func (m *browser) cancelRequest(key requestKey) {
	for activeKey, active := range m.inFlight {
		if activeKey.kind == key.kind && (key.appID == "" || activeKey.appID == key.appID) {
			active.cancel()
			delete(m.inFlight, activeKey)
		}
	}
}

func (m *browser) markCurrentUsageStale(appID string) {
	if m.detailUsage != nil && m.usageAppID == appID {
		m.detailUsage.Failed = UsageMetricFailures{CPU: true, RAM: true, Disk: true}
	}
}

func (m *browser) invalidateAppReads() {
	m.selectionGeneration++
	m.activeAppView = appViewSummary
	m.cancelRequest(requestKey{kind: requestSummary})
	m.cancelRequest(requestKey{kind: requestUsage})
	m.cancelRequest(requestKey{kind: requestPVCs})
	m.cancelRequest(requestKey{kind: requestUsageHistory})
	m.cancelRequest(requestKey{kind: requestPods})
	m.cancelRequest(requestKey{kind: requestLogs})
	m.cancelRequest(requestKey{kind: requestBuilds})
	m.cancelRequest(requestKey{kind: requestBuild})
	m.cancelRequest(requestKey{kind: requestDomains})
	m.cancelRequest(requestKey{kind: requestPorts})
	m.detailState.reset()
	m.usageState.reset()
	m.pvcsState.reset()
	m.appPVCs = nil
	m.historyState.reset()
	m.podsState.reset()
	m.podList = nil
	m.clearLogs()
	m.buildsState.reset()
	m.buildDetailState.reset()
	m.builds = nil
	m.buildDetail = nil
	m.buildMaximized = false
	m.buildSelectedID = ""
	m.buildListCursor = 0
	m.buildScroll = 0
	m.buildNotice = ""
	m.cicdToken, m.cicdNotice = "", ""
	m.cicdRevealKey, m.cicdTokenReveal, m.cicdLoading = "", false, false
	m.cicdGeneration++
	m.cicdSettingsOriginal, m.cicdSettingsDraft = client.GitBuildSettings{}, client.GitBuildSettings{}
	m.cicdSettingsLoaded, m.cicdReview, m.cicdSaving = false, false, false
	m.cicdSettingsNotice, m.cicdHistoryFocus = "", false
	m.cicdEditField, m.cicdEditValue, m.cicdEditKey = -1, "", ""
	m.history = nil
	m.envState.reset()
	m.domainState.reset()
	m.portState.reset()
	m.portConfig, m.portOriginal = PortConfig{}, PortConfig{}
	m.domainConfig = DomainConfig{}
	m.domainOriginal = DomainConfig{}
	m.domainMessage = ""
	m.domainConfirm = false
	m.domainEditing = false
	m.domainBusy = false
	m.envBusy = false
	m.envPlain = nil
	m.envSecretNames = nil
	m.envMessage = ""
	m.envEditMode = envEditNone
	m.envEditName, m.envEditValue = "", ""
	m.envRevealName, m.envRevealValue = "", ""
	m.envConfirm = false
	m.clearPendingEnv()
	m.terminalPod = ""
	m.terminalContainer = ""
	m.historyPod = ""
	m.historyPodChosen = false
	m.usageScroll = 0
	m.summaryScroll = 0
	m.detailUsage = nil
	m.usageAppID = ""
}
