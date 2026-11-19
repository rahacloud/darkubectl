package browser

import (
	"context"
	"time"
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
		if keepProfile && key.kind == requestProfile {
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
	m.cancelRequest(requestKey{kind: requestUsageHistory})
	m.cancelRequest(requestKey{kind: requestPods})
	m.cancelRequest(requestKey{kind: requestLogs})
	m.detailState.reset()
	m.usageState.reset()
	m.historyState.reset()
	m.podsState.reset()
	m.podList = nil
	m.clearLogs()
	m.history = nil
	m.historyPod = ""
	m.historyPodChosen = false
	m.usageScroll = 0
	m.summaryScroll = 0
	m.detailUsage = nil
	m.usageAppID = ""
}
