package cmd

import (
	"errors"
	"maps"
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
	// Compare the key set rather than indexing with a literal: staticcheck's
	// SA1008 rightly objects to a non-canonical constant index into an
	// http.Header, and the property under test is which spellings exist.
	got := make(map[string]string, len(h))
	for name, values := range h {
		if len(values) != 1 {
			t.Fatalf("header %q has %d values, want 1", name, len(values))
		}
		got[name] = values[0]
	}
	want := map[string]string{"x-otp": "123456", "X-Thing": "a:b"}
	if !maps.Equal(got, want) {
		t.Errorf("headers = %v, want %v (names kept exactly as typed)", got, want)
	}
	// A canonical lookup must find nothing: the value is stored under the
	// literal name, and http.Header.Get canonicalises its argument. That
	// asymmetry is what the fix relies on, not an accident of it.
	if v := h.Get("X-Otp"); v != "" {
		t.Errorf("h.Get(%q) = %q, want empty: the value is stored as %q", "X-Otp", v, "x-otp")
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
