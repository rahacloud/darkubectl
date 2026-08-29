package cmd

import (
	"errors"
	"maps"
	"testing"
)

func TestCanonicalSvcTypeAcceptsAnyCasing(t *testing.T) {
	t.Parallel()

	for _, arg := range []string{"LoadBalancer", "loadbalancer", "LOADBALANCER", "  LoadBalancer  "} {
		got, err := canonicalSvcType(arg)
		if err != nil {
			t.Fatalf("canonicalSvcType(%q) returned %v", arg, err)
		}
		if got != "LoadBalancer" {
			t.Errorf("canonicalSvcType(%q) = %q, want LoadBalancer", arg, got)
		}
	}
}

func TestCanonicalSvcTypeRejectsUnknown(t *testing.T) {
	t.Parallel()

	// ExternalName is a real Kubernetes type the Darkube chart does not offer,
	// so it is the mistake most likely to be made in earnest.
	_, err := canonicalSvcType("ExternalName")
	if !errors.Is(err, errSvcTypeArgs) {
		t.Fatalf("err = %v, want errSvcTypeArgs", err)
	}
	if _, err := canonicalSvcType(""); !errors.Is(err, errSvcTypeArgs) {
		t.Errorf("empty arg: err = %v, want errSvcTypeArgs", err)
	}
}

// app builds a normalized app object of the shape UpdateApp hands to a mutator.
func appWithSvc(svc map[string]any) map[string]any {
	return map[string]any{"name": "postgres-dev", "replicas": 1, "svc": svc}
}

func fullSvc() map[string]any {
	return map[string]any{
		"type":            "ClusterIP",
		"internalAddress": "postgres-dev.talaland-dev.svc",
		"ports": map[string]any{
			"main": map[string]any{"containerPort": 5432, "servicePort": 5432, "protocol": "TCP"},
		},
	}
}

// The reason this command exists rather than a `patch app -p '{"svc":…}'`: the
// ports must survive, because an app with none gets no Service at all.
func TestApplySvcTypeKeepsPortsAndInternalAddress(t *testing.T) {
	t.Parallel()

	svc := fullSvc()
	before := maps.Clone(svc)
	raw := appWithSvc(svc)

	if err := applySvcType(raw, "LoadBalancer"); err != nil {
		t.Fatalf("applySvcType returned %v", err)
	}

	got, _ := raw["svc"].(map[string]any)
	if got["type"] != "LoadBalancer" {
		t.Errorf("type = %v, want LoadBalancer", got["type"])
	}
	if got["internalAddress"] != before["internalAddress"] {
		t.Errorf("internalAddress = %v, want it preserved", got["internalAddress"])
	}
	ports, ok := got["ports"].(map[string]any)
	if !ok || len(ports) != 1 {
		t.Fatalf("ports = %v, want the single main port preserved", got["ports"])
	}
	main, ok := ports["main"].(map[string]any)
	if !ok || main["containerPort"] != 5432 || main["servicePort"] != 5432 {
		t.Errorf("main port = %v, want it untouched", ports["main"])
	}
}

// Nothing else on the app may move: a full-object PUT rewrites every field it
// is handed, so a mutator with a wider blast radius than advertised is the
// failure mode worth guarding.
func TestApplySvcTypeTouchesNothingElse(t *testing.T) {
	t.Parallel()

	raw := appWithSvc(fullSvc())
	if err := applySvcType(raw, "NodePort"); err != nil {
		t.Fatalf("applySvcType returned %v", err)
	}
	if raw["name"] != "postgres-dev" || raw["replicas"] != 1 {
		t.Errorf("app fields outside svc changed: %v", raw)
	}
}

func TestApplySvcTypeRejectsMissingSvc(t *testing.T) {
	t.Parallel()

	if err := applySvcType(map[string]any{"name": "x"}, "LoadBalancer"); !errors.Is(err, errNoSvc) {
		t.Errorf("err = %v, want errNoSvc", err)
	}
}

// A LoadBalancer in front of an app that declares no ports is a public address
// pointing at nothing, and the API accepts it without complaint.
func TestApplySvcTypeRejectsPortlessApp(t *testing.T) {
	t.Parallel()

	for name, svc := range map[string]map[string]any{
		"absent": {"type": "ClusterIP"},
		"empty":  {"type": "ClusterIP", "ports": map[string]any{}},
	} {
		if err := applySvcType(appWithSvc(svc), "LoadBalancer"); !errors.Is(err, errNoSvcPorts) {
			t.Errorf("%s ports: err = %v, want errNoSvcPorts", name, err)
		}
	}
}

func TestSvcTypeOf(t *testing.T) {
	t.Parallel()

	if got := svcTypeOf(appWithSvc(fullSvc())); got != "ClusterIP" {
		t.Errorf("svcTypeOf = %q, want ClusterIP", got)
	}
	if got := svcTypeOf(map[string]any{"name": "x"}); got != "" {
		t.Errorf("svcTypeOf with no svc = %q, want empty", got)
	}
}

// The endpoint that answers is the nodePort on the shared gateway, not the
// servicePort — so the report must read nodePort or it sends people to a port
// that refuses connections.
func TestExposureOfUsesNodePortAndExternalAddress(t *testing.T) {
	t.Parallel()

	raw := map[string]any{"svc": map[string]any{
		"type":            "LoadBalancer",
		"externalAddress": "8aac9b18.hsvc.ir",
		"externalIP":      "62.220.126.92",
		"internalAddress": "mssql-dev.talaland-dev.svc",
		"ports": map[string]any{
			"main": map[string]any{
				"containerPort": float64(1433), "servicePort": float64(1433), "nodePort": float64(32048),
			},
		},
	}}

	host, endpoints := exposureOf(raw)
	if host != "8aac9b18.hsvc.ir" {
		t.Errorf("host = %q, want the externalAddress in preference to the raw IP", host)
	}
	if len(endpoints) != 1 {
		t.Fatalf("endpoints = %v, want one", endpoints)
	}
	if endpoints[0].nodePort != 32048 || endpoints[0].containerPort != 1433 {
		t.Errorf("endpoint = %+v, want nodePort 32048 / containerPort 1433", endpoints[0])
	}
}

func TestExposureOfFallsBackToExternalIP(t *testing.T) {
	t.Parallel()

	host, _ := exposureOf(map[string]any{"svc": map[string]any{"externalIP": "62.220.126.92"}})
	if host != "62.220.126.92" {
		t.Errorf("host = %q, want the externalIP when no name is published", host)
	}
}

// Immediately after the write the platform has not allocated anything yet, and
// reporting a bare hostname with no port would be worse than saying nothing.
func TestExposureOfSkipsPortsWithNoNodePort(t *testing.T) {
	t.Parallel()

	raw := map[string]any{"svc": map[string]any{
		"externalAddress": "pending.hsvc.ir",
		"ports": map[string]any{
			"main": map[string]any{"containerPort": float64(1433), "servicePort": float64(1433)},
		},
	}}
	if _, endpoints := exposureOf(raw); len(endpoints) != 0 {
		t.Errorf("endpoints = %v, want none until a nodePort is allocated", endpoints)
	}
}

func TestExposureOfHandlesMissingSvc(t *testing.T) {
	t.Parallel()

	if host, endpoints := exposureOf(map[string]any{}); host != "" || endpoints != nil {
		t.Errorf("exposureOf({}) = %q/%v, want empty", host, endpoints)
	}
}
