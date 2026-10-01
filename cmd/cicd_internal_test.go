package cmd

import (
	"testing"

	"github.com/rahacloud/darkubectl/internal/client"
)

func TestCIOutput(t *testing.T) {
	t.Parallel()

	s := &client.CICDScripts{
		Envs:     map[string]string{"B_TOKEN": "abc-123", "A_AUTH": `{"auths": {}}`},
		GitLabCI: "job:\n  script: [x]",
		Curl:     "curl -X PUT …\n",
	}
	if got := ciOutput(s, ciGitLab); got != "job:\n  script: [x]\n" {
		t.Errorf("gitlab = %q", got)
	}
	if got := ciOutput(s, ciCurl); got != "curl -X PUT …\n" {
		t.Errorf("curl = %q", got)
	}
	if got := ciOutput(s, ciEnv); got != "A_AUTH='{\"auths\": {}}'\nB_TOKEN=abc-123\n" {
		t.Errorf("env = %q", got)
	}
	if got := ciOutput(s, ciGitHub); got != "" {
		t.Errorf("empty github = %q", got)
	}
}

func TestEnvValue(t *testing.T) {
	t.Parallel()

	cases := map[string]string{"plain-1": "plain-1", "it's": `'it'\''s'`, "": "''", "a b": "'a b'"}
	for in, want := range cases {
		if got := envValue(in); got != want {
			t.Errorf("envValue(%q) = %q, want %q", in, got, want)
		}
	}
}
