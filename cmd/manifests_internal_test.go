package cmd

import (
	"strings"
	"testing"
)

const sampleManifests = `---
# Source: darkube-stateless/templates/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
---
# Source: darkube-stateless/templates/secrets.yaml

---
# Source: darkube-stateless/templates/service.yaml
apiVersion: v1
kind: Service
metadata:
  name: web
`

func TestManifestDocsDropsEmptyAndFiltersKind(t *testing.T) {
	t.Parallel()

	all := manifestDocs(sampleManifests, "")
	if len(all) != 2 {
		t.Fatalf("got %d documents, want 2 (the empty secrets template dropped): %q", len(all), all)
	}
	svc := manifestDocs(sampleManifests, "service")
	if len(svc) != 1 || !strings.Contains(svc[0], "kind: Service") || !strings.HasPrefix(svc[0], "# Source:") {
		t.Errorf("service = %q", svc)
	}
	if got := manifestDocs(sampleManifests, "Ingress"); len(got) != 0 {
		t.Errorf("ingress = %q", got)
	}
}
