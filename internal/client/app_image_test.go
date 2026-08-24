package client_test

import (
	"testing"

	"github.com/rahacloud/darkubectl/internal/client"
)

func TestAppImage(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		app  client.App
		want string
	}{
		{"repo and tag", client.App{ImageRepo: "postgres", ImageTag: "16-alpine"}, "postgres:16-alpine"},
		{"repo only", client.App{ImageRepo: "postgres"}, "postgres"},
		{"nothing", client.App{}, ""},
		{"tag without repo", client.App{ImageTag: "16-alpine"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.app.Image(); got != tc.want {
				t.Errorf("Image() = %q, want %q", got, tc.want)
			}
		})
	}
}
