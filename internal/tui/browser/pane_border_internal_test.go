package browser

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestPaneFooterPreservesBorderWidth(t *testing.T) {
	t.Parallel()
	for _, width := range []int{10, 24, 40} {
		lines := renderPaneWithFooter(nil, width, 1, "Apps", false, "Use --allow-writes")
		for _, line := range lines {
			if got := ansi.StringWidth(line); got != width+2 {
				t.Fatalf("pane width %d: line width = %d", width, got)
			}
		}
	}
}
