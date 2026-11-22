package browser

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rahacloud/darkubectl/internal/client"
)

func TestSanitizeBuildOutputPreservesLines(t *testing.T) {
	t.Parallel()
	got := sanitizeBuildOutput("one\r\n\x1b[31mtwo\x1b[0m\rthree\n\nend\x00")
	if got != "one\ntwo\nthree\n\nend " {
		t.Fatalf("sanitized output = %q", got)
	}
}

func TestWrapBuildOutputPreservesTextAndWidth(t *testing.T) {
	t.Parallel()
	for _, line := range wrapBuildOutput("abcd\u4e16\u754cxyz\n\nsecond line", 5) {
		if ansi.StringWidth(line) > 5 {
			t.Fatalf("line exceeds width: %q", line)
		}
	}
	got := strings.Join(wrapBuildOutput("abcdefgh\n\nxyz", 4), "|")
	if got != "abcd|efgh||xyz" {
		t.Fatalf("wrapped lines = %q", got)
	}
}

func TestGroupedBuildOutputPreservesInterleavedOrder(t *testing.T) {
	t.Parallel()
	input := "preamble\n#1 first\n#1 progress\n#2 second\n#1 DONE 1s\nplain"
	grouped, offsets := formatBuildOutput(input, 80, false)
	for i := range grouped {
		grouped[i] = ansi.Strip(grouped[i])
	}
	for i, want := range []string{"preamble", "#1 first", "    progress", "#2 second", "#1 DONE 1s", "plain"} {
		if grouped[offsets[i]] != want {
			t.Fatalf("line %d = %q, want %q", i, grouped[offsets[i]], want)
		}
	}
	raw, _ := formatBuildOutput(input, 80, true)
	if strings.Join(raw, "\n") != input {
		t.Fatal("raw mode changed output")
	}
}

func TestBuildOutputPhaseHeadings(t *testing.T) {
	t.Parallel()
	input := "--- starting to build image\nBuilding with BuildKit\n#1 step\n#1 DONE 1s\n" +
		"--- starting to deploy\nStart deploying...\n-- unchanged"
	grouped, offsets := formatBuildOutput(input, 80, false)
	for i := range grouped {
		grouped[i] = ansi.Strip(grouped[i])
	}
	for i, want := range []string{"◆ starting to build image", "  Building with BuildKit", "#1 step", "    DONE 1s",
		"◆ starting to deploy", "  Start deploying...", "  -- unchanged"} {
		if grouped[offsets[i]] != want {
			t.Fatalf("line %d = %q, want %q", i, grouped[offsets[i]], want)
		}
	}
	raw, _ := formatBuildOutput(input, 80, true)
	if strings.Join(raw, "\n") != input {
		t.Fatal("raw mode changed phase lines")
	}
}

func TestBuildSearchAccountsForWrappedLines(t *testing.T) {
	t.Parallel()
	m := &browser{width: 80, height: 30, buildSearch: "target", buildMatch: -1,
		buildDetail: &client.Build{Log: strings.Repeat("a", 120) + "\ntarget"}, apps: []client.App{{ID: "app"}}}
	width := m.buildOutputWidth()
	m.moveBuildMatch(1)
	if m.buildScroll < len(wrapBuildOutput(strings.Repeat("a", 120), width)) {
		t.Fatalf("search scroll %d does not reach wrapped match", m.buildScroll)
	}
}

func TestBuildViewportScrollsWrappedOutput(t *testing.T) {
	t.Parallel()
	m := &browser{width: 90, height: 30, activeAppView: appViewBuilds, detail: true,
		buildDetail: &client.Build{Log: "first\n" + strings.Repeat("x", 200) + "\nlast"},
		apps:        []client.App{{ID: "app", BuildMethod: client.BuildMethodWebhook}}}
	lines := m.buildDetailLines()
	if _, _, handled := m.handleKeyCICDMode(tea.KeyPressMsg{Code: tea.KeyDown}); handled {
		t.Fatal("settings navigation intercepted build output scroll")
	}
	height := buildDetailStickyLines(lines) + 2
	first := strings.Join(m.summaryViewport(lines, height), "\n")
	if !strings.Contains(first, "first") || strings.Contains(first, "last") {
		t.Fatalf("unexpected initial viewport: %q", first)
	}
	m.buildScroll = 1000
	last := strings.Join(m.summaryViewport(lines, height), "\n")
	if !strings.Contains(last, "last") || m.buildScroll == 0 {
		t.Fatalf("last line unreachable, scroll %d: %q", m.buildScroll, last)
	}
	m.buildScroll = 1000
	short := m.summaryViewport(lines, 4)
	if len(short) != 4 || !strings.Contains(strings.Join(short, "\n"), "last") {
		t.Fatalf("short viewport cannot scroll to last line: %q", short)
	}
}

func TestBuildOutputMaximize(t *testing.T) {
	t.Parallel()
	m := &browser{page: pageApps, width: 120, height: 30, activeAppView: appViewBuilds, detail: true,
		buildDetail: &client.Build{Log: strings.Repeat("x", 100)}, apps: []client.App{{ID: "app"}}}
	regularWidth := m.buildOutputWidth()
	m.handleKeyPressViewPart5(tea.KeyPressMsg{Code: 'm', Text: "m"})
	if !m.appViewMaximized() || m.buildOutputWidth() <= regularWidth {
		t.Fatal("maximize did not expand build output")
	}
	m.handleBrowserWheel(tea.MouseWheelMsg{X: 5, Y: m.browserBodyY() + 1, Button: tea.MouseWheelDown})
	if m.buildScroll == 0 {
		t.Fatal("wheel did not scroll maximized build output")
	}
	m.handleKeyPressViewPart5(tea.KeyPressMsg{Code: 'm', Text: "m"})
	if m.appViewMaximized() || m.buildOutputWidth() != regularWidth {
		t.Fatal("restore did not return to the original width")
	}
}
