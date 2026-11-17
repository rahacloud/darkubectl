package client

import "testing"

// The support service compares the upload's type with its allowlist as a whole
// string, so a parameter such as "; charset=utf-8" turns an allowed text file
// into a refused one.
func TestAttachmentMimeTypeIsBare(t *testing.T) {
	t.Parallel()

	for path, want := range map[string]string{
		"notes.txt":       "text/plain",
		"dir/shot.PNG":    "image/png",
		"report.pdf":      "application/pdf",
		"core.dump.xyzzy": "application/octet-stream",
		"no-extension":    "application/octet-stream",
	} {
		if got := attachmentMimeType(path); got != want {
			t.Errorf("attachmentMimeType(%q) = %q, want %q", path, got, want)
		}
	}
}
