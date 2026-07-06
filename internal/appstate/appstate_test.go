package appstate_test

import (
	"testing"

	"github.com/rahacloud/darkubectl/internal/appstate"
)

func TestParsePodsFromAppPodsUpdate(t *testing.T) {
	t.Parallel()

	// A real /ws/app-pods/ frame.
	frame := []byte(`{"type":"app_pods_update","app_id":"f4f16eef","data":[` +
		`{"pod_name":"grafana-6c66b45548-92l7s","containers":[` +
		`{"name":"main","text":"running","is_ready":true,"restart_count":0}],` +
		`"text":"running","namespace":"talaland","phase":"Running"}]}`)

	pods := appstate.ParsePods(frame)
	if len(pods) != 1 {
		t.Fatalf("ParsePods returned %d pods, want 1", len(pods))
	}
	if pods[0].Name != "grafana-6c66b45548-92l7s" {
		t.Errorf("pod name = %q, want grafana-6c66b45548-92l7s", pods[0].Name)
	}
	if len(pods[0].Containers) != 1 || pods[0].Containers[0] != "main" {
		t.Errorf("containers = %v, want [main]", pods[0].Containers)
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
