package cmd

import (
	"context"
	"strings"
	"testing"
)

func TestMockModeRejectsConnectionFlags(t *testing.T) {
	t.Parallel()
	for _, name := range []string{flagConfig, flagToken, flagOrg, flagBaseURL} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := NewApp().Run(context.Background(), []string{
				"darkubectl", "--" + name, "explicit-value", "tui", "--mock-file", "demo/mock-profile.yaml",
			})
			if err == nil || !strings.Contains(err.Error(), "--"+name) {
				t.Fatalf("expected --%s rejection, got %v", name, err)
			}
		})
	}
}
