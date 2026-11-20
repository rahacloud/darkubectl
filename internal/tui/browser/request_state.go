package browser

import "time"

type requestStatus uint8

const (
	requestIdle requestStatus = iota
	requestLoading
	requestReady
	requestEmpty
	requestRefreshing
	requestStale
	requestFailed
	requestPartial
	requestUnavailable
)

func (s requestStatus) String() string {
	switch s {
	case requestLoading:
		return "Loading"
	case requestReady:
		return "Ready"
	case requestEmpty:
		return "Empty"
	case requestRefreshing:
		return "Refreshing"
	case requestStale:
		return "Stale"
	case requestFailed:
		return "Failed"
	case requestPartial:
		return "Partial"
	case requestUnavailable:
		return "Unavailable"
	default:
		return "Idle"
	}
}

type requestMeta struct {
	tenant          string
	appID           string
	scopeGeneration uint64
	requestID       uint64
}

type requestState struct {
	status        requestStatus
	meta          requestMeta
	startedAt     time.Time
	lastSuccessAt time.Time
	err           error
}

func (s *requestState) begin(meta requestMeta, now time.Time) {
	status := requestLoading
	if !s.lastSuccessAt.IsZero() {
		status = requestRefreshing
	}
	s.status = status
	s.meta = meta
	s.startedAt = now
	s.err = nil
}

func (s *requestState) succeed(empty bool, now time.Time) {
	if empty {
		s.status = requestEmpty
	} else {
		s.status = requestReady
	}
	s.startedAt = time.Time{}
	s.lastSuccessAt = now
	s.err = nil
}

func (s *requestState) fail(err error) {
	s.status = requestFailed
	if !s.lastSuccessAt.IsZero() {
		s.status = requestStale
	}
	s.startedAt = time.Time{}
	s.err = err
}

func (s *requestState) unavailable(err error) {
	s.status = requestUnavailable
	s.startedAt = time.Time{}
	s.err = err
}

func (s *requestState) partial(now time.Time) {
	s.status = requestPartial
	s.startedAt = time.Time{}
	s.lastSuccessAt = now
	s.err = nil
}

func (s *requestState) reset() {
	*s = requestState{}
}

func (s *requestState) matches(meta requestMeta) bool {
	return s.meta == meta && (s.status == requestLoading || s.status == requestRefreshing)
}

func (s *requestState) active() bool {
	return s.status == requestLoading || s.status == requestRefreshing
}
