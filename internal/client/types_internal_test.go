package client

import "testing"

func TestIsManagedService(t *testing.T) {
	t.Parallel()

	// There is no separate managed-services API: a oneclick Redis or Grafana is an
	// ordinary app whose creation_method names the service.
	managed := []string{"redisnew", "postgresqlnew", "grafana", "prometheus", "metabase", "kafka"}
	for _, m := range managed {
		if !(App{CreationMethod: m}).IsManagedService() {
			t.Errorf("creation_method %q should count as a managed service", m)
		}
	}
	plain := []string{CreationMethodDockerImage, CreationMethodGitRepoURL, "file_upload", ""}
	for _, m := range plain {
		if (App{CreationMethod: m}).IsManagedService() {
			t.Errorf("creation_method %q should not count as a managed service", m)
		}
	}
}
