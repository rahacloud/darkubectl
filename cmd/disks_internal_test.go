package cmd

import (
	"testing"

	"github.com/rahacloud/darkubectl/internal/client"
)

func TestDiskOwner(t *testing.T) {
	t.Parallel()

	apps := []string{"my-api", "my-api-worker", "redis"}
	cases := map[string]string{
		"my-api-data":        "my-api",
		"my-api-worker-data": "my-api-worker",
		"redis-data":         "redis",
		"my-apidata":         "",
		"tmp-probe-data":     "",
		"tld-db-0-production-data-stolon-keeper-0": "",
	}
	for pvc, want := range cases {
		if got := diskOwner(pvc, apps); got != want {
			t.Errorf("diskOwner(%q) = %q, want %q", pvc, got, want)
		}
	}
	if got := diskOwner("tld-db-0-production-data-stolon-keeper-1", []string{"tld-db-0-production"}); got != "tld-db-0-production" {
		t.Errorf("database disk = %q", got)
	}
}

func TestDatabaseSize(t *testing.T) {
	t.Parallel()

	d := client.Database{Nodes: []client.DatabaseNode{{CPU: 1000, RAM: 2000, Disk: 30}}}
	if got := databaseSize(d); got != "1000m / 2000M / 30Gi" {
		t.Errorf("databaseSize = %q", got)
	}
	if got := databaseSize(client.Database{}); got != "-" {
		t.Errorf("no nodes = %q", got)
	}
}
