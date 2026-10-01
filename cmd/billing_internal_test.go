package cmd

import "testing"

func TestAmount(t *testing.T) {
	t.Parallel()

	cases := map[float64]string{0: "0", 999: "999", 1000: "1,000", 1234567: "1,234,567", -7654321: "-7,654,321", 12.6: "13"}
	for in, want := range cases {
		if got := amount(in); got != want {
			t.Errorf("amount(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestShortDate(t *testing.T) {
	t.Parallel()

	if got := shortDate("2026-09-22T20:30:00Z"); got != "2026-09-22" {
		t.Errorf("shortDate = %q", got)
	}
	if got := shortDate(""); got != "-" {
		t.Errorf("empty = %q", got)
	}
}
