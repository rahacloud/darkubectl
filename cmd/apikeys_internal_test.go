package cmd

import (
	"errors"
	"testing"

	"github.com/rahacloud/darkubectl/internal/client"
)

func TestFindAPIKey(t *testing.T) {
	t.Parallel()

	keys := []client.APIKeyInfo{{ID: "1", Name: "ci"}, {ID: "2", Name: "dup"}, {ID: "3", Name: "dup"}}
	if k, err := findAPIKey(keys, "ci"); err != nil || k.ID != "1" {
		t.Errorf("by name = %+v, %v", k, err)
	}
	if k, err := findAPIKey(keys, "3"); err != nil || k.ID != "3" {
		t.Errorf("by id = %+v, %v", k, err)
	}
	if _, err := findAPIKey(keys, "dup"); !errors.Is(err, errAmbiguousKey) {
		t.Errorf("dup: err = %v", err)
	}
	if _, err := findAPIKey(keys, "nope"); !errors.Is(err, errNoSuchKey) {
		t.Errorf("nope: err = %v", err)
	}
}
