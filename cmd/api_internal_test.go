package cmd

import (
	"errors"
	"net/textproto"
	"testing"
)

func TestParseAPIHeaders(t *testing.T) {
	t.Parallel()

	h, err := parseAPIHeaders([]string{"x-otp: 123456", "X-Thing:a:b"})
	if err != nil {
		t.Fatal(err)
	}
	// Names are stored exactly as typed, NOT canonicalised. Creating an
	// object-storage key with the OTP sent as "X-Otp" answers 403 "2fa code
	// is not correct", while the identical code in a lowercase "x-otp"
	// succeeds -- that endpoint matches the name case-sensitively. So the
	// map is indexed directly and h.Get, which canonicalises its argument,
	// deliberately does not find these.
	if got := h["x-otp"]; len(got) != 1 || got[0] != "123456" {
		t.Errorf("h[%q] = %v, want [123456]", "x-otp", got)
	}
	if got := h[textproto.CanonicalMIMEHeaderKey("x-otp")]; got != nil {
		t.Errorf("canonical %q should be absent, got %v", "X-Otp", got)
	}
	if got := h.Get("x-otp"); got != "" {
		t.Errorf("h.Get = %q; values must be stored uncanonicalised", got)
	}
	if got := h["X-Thing"]; len(got) != 1 || got[0] != "a:b" {
		t.Errorf("h[%q] = %v, want the value after the first colon", "X-Thing", got)
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
