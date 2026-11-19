package client_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/rahacloud/darkubectl/internal/client"
	"gopkg.in/yaml.v3"
)

func TestAppReplicaPresenceFromJSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		json        string
		wantValue   int
		wantPresent bool
	}{
		{name: "omitted", json: `{}`, wantValue: 0, wantPresent: false},
		{name: "null", json: `{"replicas":null}`, wantValue: 0, wantPresent: false},
		{name: "explicit zero", json: `{"replicas":0}`, wantValue: 0, wantPresent: true},
		{name: "positive", json: `{"replicas":3}`, wantValue: 3, wantPresent: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var app client.App
			if err := json.Unmarshal([]byte(test.json), &app); err != nil {
				t.Fatal(err)
			}
			if app.Replicas != test.wantValue || app.ReplicasPresent != test.wantPresent {
				t.Fatalf("replicas=(%d,present=%t), want (%d,present=%t)", app.Replicas, app.ReplicasPresent, test.wantValue, test.wantPresent)
			}
		})
	}
}

func TestAppSummaryFieldsDecodeWithoutInputValues(t *testing.T) {
	t.Parallel()
	data := []byte(`{
        "disk":{"size_in_Gi":10},"external_hosts":["example.test"],
		"svc":{"type":"LoadBalancer",
        "ports":{"http":{"containerPort":8080,"servicePort":80,"nodePort":30421,"protocol":"TCP"}},
        "internalAddress":"api.prod.svc","externalIP":"192.0.2.42"},
		"readiness_probe_path":"/healthz","command":"/app/server","args":"--port 8080",
		"custom_config":{"container":{"readinessProbe":{"httpGet":{"path":"/ready","port":8080}}}}
	}`)
	var app client.App
	if err := json.Unmarshal(data, &app); err != nil {
		t.Fatal(err)
	}
	if app.Disk == nil || app.Disk.SizeInGi != 10 || !reflect.DeepEqual(app.ExternalHosts, []string{"example.test"}) {
		t.Fatalf("disk or external hosts missing: %+v", app)
	}
	if app.Service == nil ||
		app.Service.Type != "LoadBalancer" ||
		app.Service.Ports["http"].NodePort != 30421 ||
		app.Service.InternalAddress != "api.prod.svc" ||
		app.Service.ExternalIP != "192.0.2.42" {
		t.Fatalf("service projection = %+v", app.Service)
	}
	if app.ReadinessProbePath != "/healthz" ||
		app.Command != "/app/server" ||
		app.Args != "--port 8080" ||
		app.CustomConfig == nil ||
		app.CustomConfig.Container == nil ||
		app.CustomConfig.Container.ReadinessProbe.HTTPGet.Path != "/ready" {
		t.Fatalf("runtime settings projection is incomplete: %+v", app)
	}

	assertAppStructuredOutputCompatibility(t, app)
}

func assertAppStructuredOutputCompatibility(t *testing.T, app client.App) {
	t.Helper()
	jsonOutput, err := json.Marshal(app)
	if err != nil {
		t.Fatal(err)
	}
	yamlOutput, err := yaml.Marshal(app) //nolint:musttag // Verify existing YAML output compatibility.
	if err != nil {
		t.Fatal(err)
	}
	for _, output := range [][]byte{jsonOutput, yamlOutput} {
		for _, omitted := range []string{"svc:",
			"svc\"",
			"command:",
			"command\"",
			"custom_config",
			"inputs",
			"readiness_probe_path",
			"ReplicasPresent", "disk", "external_hosts", "externalhosts", "defaultoutgoingip"} {
			if strings.Contains(string(output), omitted) {
				t.Errorf("new TUI field %q leaked into structured CLI output: %s", omitted, output)
			}
		}
	}
}

func TestListAndGetTypedPreserveReplicaPresence(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v2/darkube/apps/" {
			_, _ = io.WriteString(w, `{"count":2,
"next":null,
"results":[{"id":"zero",
"name":"zero",
"replicas":0},
{"id":"missing",
"name":"missing"}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":"detail",
"name":"detail",
"replicas":0,
"svc":{"internalAddress":"app.ns.svc",
"externalAddress":"app.hsvc.example.test",
"externalIP":"192.0.2.42"}}`)
	}))
	defer server.Close()
	c := client.New(server.URL, client.APIKey("test"), "org")
	apps, err := c.ListApps(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 2 || !apps[0].ReplicasPresent || apps[0].Replicas != 0 || apps[1].ReplicasPresent {
		t.Fatalf("ListApps did not preserve replicas presence: %+v", apps)
	}
	detail, err := c.GetAppTyped(context.Background(), "detail")
	if err != nil {
		t.Fatal(err)
	}
	if detail.ID != "detail" ||
		!detail.ReplicasPresent ||
		detail.Replicas != 0 ||
		detail.Service == nil ||
		detail.Service.InternalAddress != "app.ns.svc" ||
		detail.Service.ExternalAddress != "app.hsvc.example.test" {
		t.Fatalf("GetAppTyped did not preserve explicit zero: %+v", detail)
	}
}

func TestAppDetailOptionalFieldsAndProbeShapes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, data string
		check      func(*testing.T, client.App)
	}{
		{"missing", `{}`, checkMissingAppDetails},
		{"null", `{"svc":null,
"custom_config":null,
"command":null,
"args":null,
"readiness_probe_path":null}`, checkNullAppDetails},
		{"exec and autoscaling", `{"custom_config":{"hpa":{"enabled":true,
"minReplicas":1,
"maxReplicas":5,
"targetCPUUtilizationPercentage":75},
"container":{"readinessProbe":{"exec":{"command":["check",
"ready"]},
"periodSeconds":10}}}}`, checkExecAutoscaling},
		{"tcp probe", `{"custom_config":{"container":{"readinessProbe":{"tcpSocket":{"port":27017}}}}}`, checkTCPInputs},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var app client.App
			if err := json.Unmarshal([]byte(tc.data), &app); err != nil {
				t.Fatal(err)
			}
			tc.check(t, app)
		})
	}
}

func checkMissingAppDetails(t *testing.T, app client.App) {
	t.Helper()
	if app.Service != nil || app.CustomConfig != nil {
		t.Fatalf("unexpected values: %+v", app)
	}
}

func checkNullAppDetails(t *testing.T, app client.App) {
	t.Helper()
	if app.Service != nil || app.CustomConfig != nil || app.Command != "" {
		t.Fatalf("unexpected values: %+v", app)
	}
}

func checkExecAutoscaling(t *testing.T, app client.App) {
	t.Helper()
	if app.CustomConfig == nil ||
		app.CustomConfig.HPA == nil ||
		!app.CustomConfig.HPA.Enabled ||
		app.CustomConfig.HPA.MinReplicas != 1 ||
		app.CustomConfig.HPA.MaxReplicas != 5 ||
		app.CustomConfig.HPA.TargetCPUUtilizationPercentage != 75 ||
		app.CustomConfig.Container == nil ||
		app.CustomConfig.Container.ReadinessProbe == nil ||
		!reflect.DeepEqual(app.CustomConfig.Container.ReadinessProbe.Exec.Command,
			[]string{"check",
				"ready"}) {
		t.Fatalf("app = %+v", app)
	}
}

func checkTCPInputs(t *testing.T, app client.App) {
	t.Helper()
	if app.CustomConfig == nil ||
		app.CustomConfig.Container == nil ||
		app.CustomConfig.Container.ReadinessProbe == nil ||
		app.CustomConfig.Container.ReadinessProbe.TCPSocket == nil ||
		string(app.CustomConfig.Container.ReadinessProbe.TCPSocket.Port) != "27017" {
		t.Fatalf("app = %+v", app)
	}
}

func TestOptionalDetailsDoNotBreakList(t *testing.T) {
	t.Parallel()
	cases := []string{
		`{"custom_config":{"hpa":{"minReplicas":"1"}}}`,
		`{"custom_config":[]}`,
		`{"svc":{"ports":[]}}`,
		`{"command":[],"args":{},"readiness_probe_path":42}`,
	}
	for _, value := range cases {
		var app client.App
		if err := json.Unmarshal([]byte(value), &app); err != nil {
			t.Fatalf("%s: %v", value, err)
		}
		if app.Service != nil || app.CustomConfig != nil || app.Command != "" || app.Args != "" || app.ReadinessProbePath != "" {
			t.Fatalf("unexpected partial projection: %+v", app)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"count":2,"next":null,"results":[`+
			`{"id":"bad","svc":{"ports":[]}},`+
			`{"id":"good","svc":{"internalAddress":"ok"}}]}`)
	}))
	defer server.Close()
	apps, err := client.New(server.URL, client.APIKey("test"), "org").ListApps(context.Background())
	if err != nil || len(apps) != 2 || apps[0].Service != nil || apps[1].Service == nil || apps[1].Service.InternalAddress != "ok" {
		t.Fatalf("apps=%+v err=%v", apps, err)
	}
}
