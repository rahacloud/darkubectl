package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rahacloud/darkubectl/internal/client"
)

// version is overridable at build time via -ldflags "-X ...cmd.version=...".
var version = "dev"

// Shared command-level errors.
var (
	errMissingAppRef = errors.New("an app NAME or ID argument is required")
	errAborted       = errors.New("aborted")
)

// confirm asks the user to approve the change just described on stderr.
// Returns false on EOF or anything other than y/yes.
func confirm() bool {
	fmt.Fprint(os.Stderr, "Proceed? [y/N]: ")
	line, ok := readLine()
	if !ok {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

// confirmExact requires the user to type an exact string (e.g. a resource name).
func confirmExact(prompt, want string) bool {
	fmt.Fprint(os.Stderr, prompt)
	line, ok := readLine()
	if !ok {
		return false
	}
	return strings.TrimSpace(line) == want
}

func readLine() (string, bool) {
	r := bufio.NewReader(os.Stdin)
	line, err := r.ReadString('\n')
	if err != nil && line == "" {
		return "", false
	}
	return line, true
}

// stateLabel renders an app's live state compactly.
func stateLabel(s client.State) string {
	switch {
	case s.Text != "":
		return s.Text
	case s.StateType != "":
		return s.StateType
	default:
		return "-"
	}
}

func yesNo(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// hoursPerDay is the divisor that turns an elapsed hour count into days.
const hoursPerDay = 24

// age renders how long ago t was, in the compact kubectl style (90d, 13d, 5h,
// 12m, 45s). A zero or future timestamp renders as "-".
func age(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t)
	if d < 0 {
		return "-"
	}
	switch {
	case d < time.Minute:
		return strconv.Itoa(int(d.Seconds())) + "s"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h"
	default:
		return strconv.Itoa(int(d.Hours()/hoursPerDay)) + "d"
	}
}

// ageOf renders an API timestamp string (RFC 3339) as a relative age.
func ageOf(timestamp string) string {
	if timestamp == "" {
		return "-"
	}
	t, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return "-"
	}
	return age(t)
}

func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}
