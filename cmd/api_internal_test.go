package cmd

import (
	"errors"
	"testing"
)

func TestParseAPIHeaders(t *testing.T) {
	t.Parallel()

	h, err := parseAPIHeaders([]string{"x-otp: 123456", "X-Thing:a:b"})
	if err != nil {
		t.Fatal(err)
	}
	if got := h.Get("X-Otp"); got != "123456" {
		t.Errorf("x-otp = %q", got)
	}
	if got := h.Get("X-Thing"); got != "a:b" {
		t.Errorf("x-thing = %q, want the value after the first colon", got)
	}

	for _, bad := range []string{"no-colon", ": value"} {
		if _, err := parseAPIHeaders([]string{bad}); !errors.Is(err, errAPIHeader) {
			t.Errorf("%q: err = %v, want errAPIHeader", bad, err)
		}
	}

	if h, err := parseAPIHeaders(nil); len(h) != 0 || err != nil {
		t.Errorf("nil = %v, %v", h, err)
	}
}
