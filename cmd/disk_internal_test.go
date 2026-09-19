package cmd

import (
	"errors"
	"maps"
	"testing"
)

// appWithDisk builds a normalized app object of the shape UpdateApp hands to a
// mutator. Sizes arrive as float64 because they have been through the decoder.
func appWithDisk(disk map[string]any) map[string]any {
	return map[string]any{"name": "talaland-untagged-gitlab-runner", "replicas": 1, keyDisk: disk}
}

func TestApplyDiskSizeGrows(t *testing.T) {
	t.Parallel()

	app := appWithDisk(map[string]any{
		keyDiskSize:          float64(15),
		"storage_class_name": "rawfile-btrfs",
		"partitions":         []any{},
		"set_fsgroup":        true,
	})
	if err := applyDiskSize(app, 40); err != nil {
		t.Fatalf("applyDiskSize returned %v", err)
	}

	disk, _ := app[keyDisk].(map[string]any)
	if got := disk[keyDiskSize]; got != 40 {
		t.Errorf("size = %v, want 40", got)
	}
}

// The storage class and the partition layout are creation-only on the write
// serializer, so a resize must leave them exactly as they were read. Replacing
// the disk object wholesale — the obvious implementation — drops them.
func TestApplyDiskSizeKeepsTheRestOfTheBlock(t *testing.T) {
	t.Parallel()

	original := map[string]any{
		keyDiskSize:          float64(15),
		"storage_class_name": "rawfile-btrfs",
		"partitions":         []any{map[string]any{"mount_path": "/data"}},
		"set_fsgroup":        true,
	}
	app := appWithDisk(maps.Clone(original))
	if err := applyDiskSize(app, 40); err != nil {
		t.Fatalf("applyDiskSize returned %v", err)
	}

	disk, _ := app[keyDisk].(map[string]any)
	for _, k := range []string{"storage_class_name", "partitions", "set_fsgroup"} {
		if _, found := disk[k]; !found {
			t.Errorf("%s was dropped", k)
		}
	}
	if got, want := disk["storage_class_name"], original["storage_class_name"]; got != want {
		t.Errorf("storage_class_name = %v, want %v", got, want)
	}
}

func TestApplyDiskSizeRefusesToShrink(t *testing.T) {
	t.Parallel()

	app := appWithDisk(map[string]any{keyDiskSize: float64(120)})
	err := applyDiskSize(app, 40)
	if err == nil {
		t.Fatal("applyDiskSize accepted a shrink")
	}
	// The app must be left untouched, since the caller may still PUT it.
	disk, _ := app[keyDisk].(map[string]any)
	if got := disk[keyDiskSize]; got != float64(120) {
		t.Errorf("size = %v after a refused shrink, want 120", got)
	}
}

// Setting the size it already has is not a shrink and must be allowed through,
// so the command can report "already Ngi" rather than erroring.
func TestApplyDiskSizeAcceptsTheSameSize(t *testing.T) {
	t.Parallel()

	app := appWithDisk(map[string]any{keyDiskSize: float64(15)})
	if err := applyDiskSize(app, 15); err != nil {
		t.Fatalf("applyDiskSize returned %v", err)
	}
}

func TestApplyDiskSizeRejectsDisklessApp(t *testing.T) {
	t.Parallel()

	// A diskless app reads back as an empty object, not a missing key.
	for name, app := range map[string]map[string]any{
		"empty":   appWithDisk(map[string]any{}),
		"missing": {"name": "tld-gateway", "replicas": 2},
	} {
		if err := applyDiskSize(app, 40); !errors.Is(err, errNoDisk) {
			t.Errorf("%s: err = %v, want errNoDisk", name, err)
		}
	}
}
