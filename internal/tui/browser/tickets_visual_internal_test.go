package browser

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rahacloud/darkubectl/internal/client"
)

func TestTicketListShowsStatusGlyphsAndMetadata(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := &browser{width: 140, ticketStatus: client.TicketStatusAll, ticketMine: true, ticketRows: []client.Ticket{
		{ID: "1", Status: "Open", Summary: "مشکل اتصال", Created: "2026-11-19T10:20:00+0000", Updated: "2026-11-20T10:20:00+0000",
			RequestType: client.TicketRequestType{Key: "database"}},
		{ID: "2", Status: "Close", Summary: "Done"},
	}}
	lines := strings.Join(m.ticketPaneLines(8), "\n")
	for _, value := range []string{"●", "✓", "#1", "#2", "2026-11-20", "UPDATED", "SUBJECT", "—"} {
		if !strings.Contains(lines, value) {
			t.Errorf("missing %q in %q", value, lines)
		}
	}
	if strings.Contains(lines, "Open") || strings.Contains(lines, "Closed") {
		t.Errorf("status labels in ticket rows: %q", lines)
	}
}

func TestTicketCommentHeaderGroupsAuthorLeftAndTimeRight(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	comment := client.TicketComment{Author: client.TicketUser{DisplayName: "Sara", Email: "sara@hamravesh.com"},
		Created: "2026-11-20T10:20:00+0000"}
	header := ticketCommentHeader(comment, 50)
	if !strings.Contains(header, "●") || !strings.Contains(header, "Sara") || !strings.Contains(header, "2026-11-20") ||
		strings.Contains(header, "SUPPORT") || ansi.StringWidth(header) != 50 {
		t.Fatalf("header: %q", header)
	}
	if !strings.HasPrefix(header, "● Sara") || !strings.HasSuffix(header, ticketTimestamp(string(comment.Created))) {
		t.Fatalf("author and timestamp placement: %q", header)
	}
	customer := ticketCommentHeader(client.TicketComment{Author: client.TicketUser{DisplayName: "Ali"}}, 50)
	if !strings.Contains(customer, "◆") || !strings.Contains(customer, "Ali") || strings.Contains(customer, "YOU") {
		t.Fatalf("customer header: %q", customer)
	}
	lines := renderTicketBody("سلام\n```text\nconnection refused\n```\nپایان")
	if len(lines) != 5 || !strings.Contains(lines[2], "connection refused") || !strings.Contains(lines[4], "پایان") {
		t.Fatalf("message lines: %#v", lines)
	}
}

func TestTicketTimestampFormatsJiraAndRFC3339(t *testing.T) {
	t.Parallel()
	// These timestamps refer to the same instant, despite using different offsets
	// and the two timestamp shapes returned by the support API.
	inputs := []string{
		"2026-09-21T10:28:00.597+0000",
		"2026-09-21T13:58:00.597+0330",
		"2026-09-21T10:28:00.597Z",
		" 2026-09-21T10:28:00.597+00:00 ",
	}
	want := ticketTimestamp(inputs[0])
	if want == inputs[0] || len(want) != len("2006-01-02 15:04") {
		t.Fatalf("timestamp not formatted: %q", want)
	}
	for _, input := range inputs[1:] {
		if got := ticketTimestamp(input); got != want {
			t.Errorf("%q = %q, want %q", input, got, want)
		}
	}
	if got := ticketTimestamp(""); got != "—" {
		t.Errorf("missing timestamp = %q", got)
	}
	if got := ticketTimestamp("invalid\x1b[31m date"); got != "invalid date" {
		t.Errorf("unrecognized timestamp = %q", got)
	}
}

func TestTicketVisibleRangeKeepsSelectionInView(t *testing.T) {
	t.Parallel()
	first, last := ticketVisibleRange(30, 20, 9)
	if first > 20 || last <= 20 || last-first != 7 {
		t.Fatalf("range %d:%d", first, last)
	}
}

func TestTicketConversationScrollClampsAtEnd(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := &browser{width: 140, ticketDetail: &client.Ticket{ID: "1", Summary: "topic"}, ticketScroll: 999,
		ticketComments: []client.TicketComment{{Body: "first\nsecond\nthird\nfourth\nfifth\nsixth"}}}
	end := strings.Join(m.ticketConversationLines(4), "\n")
	if !strings.Contains(end, "sixth") || strings.Contains(end, "first") {
		t.Fatalf("wrong end of conversation: %q", end)
	}
	if m.ticketScroll <= 0 || m.ticketScroll >= 999 {
		t.Fatalf("unclamped scroll: %d", m.ticketScroll)
	}
	m.ticketScroll = 0
	start := strings.Join(m.ticketConversationLines(4), "\n")
	if !strings.Contains(start, "topic") {
		t.Fatalf("wrong start of conversation: %q", start)
	}
}

func TestTicketWheelUsesPaneUnderPointer(t *testing.T) {
	t.Parallel()
	m := &browser{page: pageTickets, width: 140, height: 30, ticketRows: []client.Ticket{{ID: "1"}, {ID: "2"}, {ID: "3"}}}
	listWidth, _, contextWidth := m.paneWidths()
	listLeft, listRight := browserWheelListBounds(listWidth, contextWidth)
	y := m.browserBodyY() + 2
	m.handleBrowserWheel(tea.MouseWheelMsg{X: listLeft + 1, Y: y, Button: tea.MouseWheelDown})
	if m.ticketCursor != 1 || m.detail {
		t.Fatalf("list wheel: cursor=%d detail=%v", m.ticketCursor, m.detail)
	}
	m.handleBrowserWheel(tea.MouseWheelMsg{X: listRight + 2, Y: y, Button: tea.MouseWheelDown})
	if m.ticketScroll != 3 || !m.detail {
		t.Fatalf("conversation wheel: scroll=%d detail=%v", m.ticketScroll, m.detail)
	}
}

func TestTicketPageKeysAndMouseFocus(t *testing.T) {
	t.Parallel()
	m := &browser{page: pageTickets, width: 140, height: 30, detail: true, ticketRows: []client.Ticket{{ID: "1"}, {ID: "2"}}}
	m.handleTicketKey(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if m.ticketScroll <= 1 {
		t.Fatalf("page down did not scroll by a page: %d", m.ticketScroll)
	}
	m.handleTicketKey(tea.KeyPressMsg{Code: tea.KeyPgUp})
	if m.ticketScroll != 0 {
		t.Fatalf("page up = %d", m.ticketScroll)
	}
	_, _, contextWidth := m.paneWidths()
	m.contextFocused = true
	m.handleBrowserMouse(tea.MouseClickMsg{X: contextWidth + 4, Y: m.browserBodyY() + 3, Button: tea.MouseLeft})
	if m.contextFocused || m.ticketCursor != 1 || m.detail {
		t.Fatalf("ticket click: focused=%v cursor=%d detail=%v", m.contextFocused, m.ticketCursor, m.detail)
	}
	listWidth, _, contextWidth := m.paneWidths()
	_, listRight := browserWheelListBounds(listWidth, contextWidth)
	m.ticketStatus = client.TicketStatusAll
	m.handleBrowserMouse(tea.MouseClickMsg{X: listRight - 12, Y: m.browserBodyY(), Button: tea.MouseLeft})
	if m.ticketStatus != client.TicketStatusOpen {
		t.Fatalf("status border click: %q", m.ticketStatus)
	}
	m.handleBrowserMouse(tea.MouseClickMsg{X: listRight - 7, Y: m.browserBodyY(), Button: tea.MouseLeft})
	if !m.ticketMine {
		t.Fatal("scope border click did not switch to Mine")
	}
}

func TestTicketControlsInPaneBorder(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := &browser{page: pageTickets, width: 140, ticketStatus: client.TicketStatusClosed, userEmail: "operator@example.test"}
	appsWidth, summaryWidth, _ := m.paneWidths()
	title, _ := m.paneTitles(appsWidth, summaryWidth)
	hasStatus := strings.Contains(title, "Closed")
	hasScope := strings.Contains(title, "Org")
	hasLegacyScope := strings.Contains(title, "o Org")
	hasLegacyStatus := strings.Contains(title, "f Closed")
	if !hasStatus || !hasScope || hasLegacyScope || hasLegacyStatus {
		t.Fatalf("ticket filter and scope absent from border: %q", title)
	}
	list := strings.Join(m.ticketPaneLines(8), "\n")
	if strings.Contains(list, "Closed") || strings.Contains(list, "Organization") {
		t.Fatalf("ticket filters duplicated in list pane: %q", list)
	}
	header := strings.Join(m.viewHeaderLines(), "\n")
	if strings.Contains(header, "Tickets  operator") || !strings.Contains(header, "operator@example.test") {
		t.Fatalf("header: %q", header)
	}
	m.page = pageDB
	title, _ = m.paneTitles(appsWidth, summaryWidth)
	if !strings.Contains(title, "Name") {
		t.Fatalf("DB sort absent from border: %q", title)
	}
}

func TestTicketNewFormShowsLabelsAndFocus(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := &browser{width: 140, tenant: "demo", ticketCompose: "new", ticketComposeField: 2,
		ticketCategory: "database", ticketCategories: []client.TicketCategory{{Key: "database", Value: "Database"}}, ticketSummary: "Pod"}
	content := strings.Join(m.ticketNewFormLines(16), "\n")
	for _, want := range []string{"FILE A SUPPORT TICKET", "Database", "Platform default", "› Subject", "Pod█", "Message"} {
		if !strings.Contains(content, want) {
			t.Errorf("missing %q in %q", want, content)
		}
	}
	m.appendTicketComposeText("p")
	if m.ticketSummary != "Podp" {
		t.Fatalf("subject input lost: %q", m.ticketSummary)
	}
	m.ticketComposeField = 3
	m.appendTicketComposeText("پیام")
	if m.ticketDraft != "پیام" {
		t.Fatalf("message input lost: %q", m.ticketDraft)
	}
}

func TestTicketPriorityShowsNameInFormAndReview(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := &browser{width: 140, ticketPriority: "10002", ticketPriorities: []client.TicketPriority{{ID: "10002", Keyword: "critical"}}}
	for _, lines := range [][]string{m.ticketNewFormLines(16), m.ticketReviewLines(16)} {
		content := strings.Join(lines, "\n")
		if !strings.Contains(content, "critical") || strings.Contains(content, "10002") {
			t.Fatalf("priority display: %q", content)
		}
	}
	if ticketPriorityColor("critical") != browserDanger || ticketPriorityColor("normal") != browserSuccess {
		t.Fatal("priority colors do not match their severity")
	}
}

func TestTicketReplyInstructionsStayInFooter(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := &browser{width: 140, ticketCompose: "reply", ticketDraft: "سلام"}
	content := strings.Join(m.ticketConversationLines(10), "\n")
	if strings.Contains(content, "Ctrl+J") || strings.Contains(content, "Enter send") {
		t.Fatalf("instructions in reply body: %q", content)
	}
	footer := m.ticketFooterLine("")
	if !strings.Contains(footer, "send") || !strings.Contains(footer, "newline") {
		t.Fatalf("missing footer controls: %q", footer)
	}
}
