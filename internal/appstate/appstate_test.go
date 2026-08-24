package appstate_test

import (
	"testing"
	"time"

	"github.com/rahacloud/darkubectl/internal/appstate"
)

func TestParsePodsFromAppPodsUpdate(t *testing.T) {
	t.Parallel()

	// A real /ws/app-pods/ frame.
	frame := []byte(`{"type":"app_pods_update","app_id":"f4f16eef","data":[` +
		`{"pod_name":"grafana-6c66b45548-92l7s","containers":[` +
		`{"name":"main","text":"running","is_ready":true,"restart_count":0}],` +
		`"text":"running","namespace":"talaland","phase":"Running","is_ready":true,` +
		`"creation_time":"2026-08-11 07:17:31+00:00"}]}`)

	pods := appstate.ParsePods(frame)
	if len(pods) != 1 {
		t.Fatalf("ParsePods returned %d pods, want 1", len(pods))
	}
	pod := pods[0]
	if pod.Name != "grafana-6c66b45548-92l7s" {
		t.Errorf("pod name = %q, want grafana-6c66b45548-92l7s", pod.Name)
	}
	names := pod.ContainerNames()
	if len(names) != 1 || names[0] != "main" {
		t.Errorf("container names = %v, want [main]", names)
	}
	if pod.Namespace != "talaland" || pod.Phase != "Running" || pod.State != "running" {
		t.Errorf("pod = %+v, want namespace/phase/state talaland/Running/running", pod)
	}
	if !pod.Ready {
		t.Error("pod Ready = false, want true")
	}
	if ready, total := pod.ReadyCount(); ready != 1 || total != 1 {
		t.Errorf("ReadyCount = %d/%d, want 1/1", ready, total)
	}
	// The frame's timestamp uses a space where RFC 3339 wants a "T".
	want := time.Date(2026, 8, 11, 7, 17, 31, 0, time.UTC)
	if !pod.CreatedAt.Equal(want) {
		t.Errorf("CreatedAt = %v, want %v", pod.CreatedAt, want)
	}
}

func TestParsePodsReadsCrashLoopHealth(t *testing.T) {
	t.Parallel()

	// A crash-looping pod: phase is still Running, and only the container's
	// restart count and last state say what is actually happening.
	frame := []byte(`{"type":"app_pods_update","data":[` +
		`{"pod_name":"lending-dev-9f47fbb44-j2vbk","namespace":"talaland-dev",` +
		`"phase":"Running","text":"error","state_type":"error","is_ready":false,` +
		`"containers":[{"name":"main","text":"error","is_ready":false,"restart_count":3529,` +
		`"last_state":{"state":"terminated","reason":"Error","message":null}}]}]}`)

	pods := appstate.ParsePods(frame)
	if len(pods) != 1 {
		t.Fatalf("ParsePods returned %d pods, want 1", len(pods))
	}
	pod := pods[0]
	if pod.Ready {
		t.Error("pod Ready = true, want false")
	}
	if ready, total := pod.ReadyCount(); ready != 0 || total != 1 {
		t.Errorf("ReadyCount = %d/%d, want 0/1", ready, total)
	}
	if got := pod.Restarts(); got != 3529 {
		t.Errorf("Restarts = %d, want 3529", got)
	}
	if got := pod.Containers[0].LastState; got != "terminated: Error" {
		t.Errorf("LastState = %q, want %q", got, "terminated: Error")
	}
}

func TestParsePodsIgnoresNonPodFrames(t *testing.T) {
	t.Parallel()

	// An app-state (aggregate) frame carries no pods.
	frame := []byte(`{"type":"app_state_update","data":{"text":"healthy","ready_replicas":1}}`)
	if pods := appstate.ParsePods(frame); pods != nil {
		t.Errorf("ParsePods = %v, want nil for a non-pod frame", pods)
	}
}

func TestParsePodsToleratesMissingCreationTime(t *testing.T) {
	t.Parallel()

	frame := []byte(`{"type":"app_pods_update","data":[{"pod_name":"p","creation_time":"not a time"}]}`)
	pods := appstate.ParsePods(frame)
	if len(pods) != 1 {
		t.Fatalf("ParsePods returned %d pods, want 1", len(pods))
	}
	if !pods[0].CreatedAt.IsZero() {
		t.Errorf("CreatedAt = %v, want the zero time for an unparsable timestamp", pods[0].CreatedAt)
	}
}
