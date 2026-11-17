package cmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/rahacloud/darkubectl/internal/client"
)

func testStorages() []client.Storage {
	return []client.Storage{
		{
			ID: "s-1", Name: "Tehran - Asiatech",
			Buckets: []client.Bucket{{ID: "b-1", Name: "media"}, {ID: "b-2", Name: "backups"}},
			Keys:    []client.StorageKey{{Name: "media-rw", Actions: []string{"Read", "Write"}, Buckets: []string{"media"}}},
		},
		{ID: "s-2", Name: "Qom - Amin", Buckets: []client.Bucket{{ID: "b-3", Name: "archive"}}},
	}
}

func TestParseKeyActions(t *testing.T) {
	t.Parallel()

	// Any case, comma-separated or repeated, de-duplicated, in display order.
	got, err := parseKeyActions([]string{"list, WRITE", "read", "Write"})
	if err != nil || strings.Join(got, ",") != "Read,Write,List" {
		t.Fatalf("got %v, %v", got, err)
	}
	if _, err := parseKeyActions([]string{"Delete"}); !errors.Is(err, errBadKeyAction) {
		t.Fatalf("an unknown action: got %v", err)
	}
	if got, err := parseKeyActions(nil); err != nil || len(got) != 0 {
		t.Fatalf("no actions: got %v, %v", got, err)
	}
}

func TestPickStorage(t *testing.T) {
	t.Parallel()

	storages := testStorages()
	for _, ref := range []string{"s-2", "qom - amin", "Qom - Amin"} {
		if s, err := pickStorage(storages, ref); err != nil || s.ID != "s-2" {
			t.Errorf("pickStorage(%q) = %v, %v", ref, s, err)
		}
	}
	// With several storages the choice is the user's, and the error names them.
	if _, err := pickStorage(storages, ""); !errors.Is(err, errPickStorage) || !strings.Contains(err.Error(), "Qom - Amin") {
		t.Errorf("no --storage with two storages: %v", err)
	}
	if _, err := pickStorage(storages, "nowhere"); !errors.Is(err, errNoSuchStorage) {
		t.Errorf("an unknown storage: %v", err)
	}
	if s, err := pickStorage(storages[:1], ""); err != nil || s.ID != "s-1" {
		t.Errorf("the only storage should be picked: %v, %v", s, err)
	}
	if _, err := pickStorage(nil, ""); !errors.Is(err, errNoStorage) {
		t.Errorf("no storage at all: %v", err)
	}
}

func TestBucketIDs(t *testing.T) {
	t.Parallel()

	storages := testStorages()
	home, ids, err := bucketIDs(storages, []string{"backups", "media"})
	if err != nil || home.ID != "s-1" || strings.Join(ids, ",") != "b-2,b-1" {
		t.Fatalf("got %v %v %v", home, ids, err)
	}
	if _, _, err := bucketIDs(storages, []string{"media", "nope"}); !errors.Is(err, errNoSuchBucket) {
		t.Errorf("an unknown bucket: %v", err)
	}
	// A key lives on one storage, so its buckets cannot come from two.
	if _, _, err := bucketIDs(storages, []string{"media", "archive"}); !errors.Is(err, errBucketsSpanStorages) {
		t.Errorf("buckets on two storages: %v", err)
	}
}

func TestFindStorageKey(t *testing.T) {
	t.Parallel()

	storage, key, err := findStorageKey(testStorages(), "media-rw")
	if err != nil || storage.ID != "s-1" || key.Name != "media-rw" {
		t.Fatalf("got %v %v %v", storage, key, err)
	}
	if _, _, err := findStorageKey(testStorages(), "gone"); !errors.Is(err, errNoSuchStorageKey) {
		t.Errorf("an unknown key: %v", err)
	}
}

func TestSameSet(t *testing.T) {
	t.Parallel()

	if !sameSet([]string{"a", "b"}, []string{"b", "a"}) {
		t.Error("order should not matter")
	}
	if sameSet([]string{"a"}, []string{"a", "b"}) || sameSet([]string{"a"}, []string{"b"}) {
		t.Error("different members compared equal")
	}
}
