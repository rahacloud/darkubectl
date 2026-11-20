package browser

import (
	"context"
	"image/color"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rahacloud/darkubectl/internal/client"
)

const (
	ticketDetailHorizontalInset = 4
	ticketMinimumContentWidth   = 12
	ticketSubjectFieldIndex     = 2
	ticketMessageFieldIndex     = 3
	ticketFormHorizontalInset   = 4
	ticketBodyVerticalInset     = 2
	ticketListMinimumWidth      = 42
	ticketListColumnGap         = 5
	ticketTimestampRightInset   = 2
	ticketNameColumnGap         = 3
	ticketPageBottomInset       = 4
	ticketComposeFieldCount     = 4
	ticketLastComposeFieldIndex = 3
	ticketControlWideSeparator  = 2
)

type ticketListResult struct {
	meta requestMeta
	rows client.TicketList
	err  error
}
type ticketThreadResult struct {
	meta     requestMeta
	ticket   client.Ticket
	comments []client.TicketComment
	err      error
}
type ticketCategoriesResult struct {
	tenant     string
	generation uint64
	categories []client.TicketCategory
	priorities []client.TicketPriority
	err        error
}
type ticketWriteResult struct {
	tenant     string
	generation uint64
	id         string
	created    bool
	err        error
}

func (m *browser) loadTickets(force bool) tea.Cmd {
	if m.page != pageTickets || m.ticketState.active() && !force {
		return nil
	}
	if m.listTickets == nil {
		m.ticketState.unavailable(errTicketListingUnavailable)
		return nil
	}
	meta, ctx := m.startRequest(requestTickets, m.tenant, "", m.tenantGeneration)
	m.ticketState.begin(meta, m.requestTime())
	tenant, status, mine, fn := meta.tenant, m.ticketStatus, m.ticketMine, m.listTickets
	return func() tea.Msg { rows, err := fn(ctx, tenant, status, mine); return ticketListResult{meta, rows, err} }
}

func (m *browser) loadTicketThread() tea.Cmd {
	if m.ticketCursor < 0 || m.ticketCursor >= len(m.visibleTickets()) {
		return nil
	}
	t := m.visibleTickets()[m.ticketCursor]
	if m.ticketSelection == string(t.ID) && (m.ticketDetailState.active() || m.ticketDetailState.status == requestReady) {
		return nil
	}
	if m.ticketSelection != string(t.ID) {
		m.ticketScroll = 0
	}
	m.ticketSelection = string(t.ID)
	m.ticketDetail = nil
	m.ticketComments = nil
	meta, ctx := m.startRequest(requestTicketThread, m.tenant, string(t.ID), m.tenantGeneration)
	m.ticketDetailState.begin(meta, m.requestTime())
	tenant, id, get, comments := meta.tenant, string(t.ID), m.getTicket, m.getTicketComments
	return func() tea.Msg {
		if get == nil || comments == nil {
			return ticketThreadResult{meta: meta, err: errTicketConversationUnavailable}
		}
		row, err := get(ctx, tenant, id)
		if err != nil {
			return ticketThreadResult{meta: meta, err: err}
		}
		thread, err := comments(ctx, tenant, id)
		return ticketThreadResult{meta: meta, ticket: row, comments: thread, err: err}
	}
}

func (m *browser) visibleTickets() []client.Ticket {
	rows := make([]client.Ticket, 0, len(m.ticketRows))
	q := strings.ToLower(strings.TrimSpace(m.ticketFilter))
	for _, t := range m.ticketRows {
		if q == "" || strings.Contains(strings.ToLower(string(t.ID)+" "+t.Summary), q) {
			rows = append(rows, t)
		}
	}
	return rows
}

func (m *browser) ticketPaneLines(height int) []string {
	width, _, _ := m.paneWidths()
	rows := m.visibleTickets()
	lines := []string{}
	if m.ticketStatus == "" {
		m.ticketStatus = client.TicketStatusAll
	}
	if m.ticketState.active() && len(m.ticketRows) == 0 {
		lines = append(lines, m.loadingMessage("Loading tickets…"))
	}
	if m.ticketState.err != nil && len(m.ticketRows) == 0 {
		lines = append(lines, paint("Could not load tickets · R retry", browserDanger, true))
	}
	if len(rows) == 0 && !m.ticketState.active() && m.ticketState.err == nil {
		if m.ticketFilter != "" {
			lines = append(lines, "No matching tickets")
		} else {
			lines = append(lines, "No tickets in this view")
		}
	}
	idWidth, subjectWidth, timeWidth := ticketColumnWidths(width)
	lines = append(lines, paint(" "+ticketTableRow(" ", "ID", "SUBJECT", "UPDATED", idWidth, subjectWidth, timeWidth), browserMuted, true))
	first, last := ticketVisibleRange(len(rows), m.ticketCursor, height)
	for i := first; i < last; i++ {
		t := rows[i]
		glyph, statusColor := ticketStatusGlyph(t.Status)
		marker := " "
		stamp := ticketTimestamp(t.Updated)
		if timeWidth < 16 && len(stamp) >= 10 {
			stamp = stamp[:10]
		}
		id := "#" + safeText(string(t.ID))
		summary := safeText(t.Summary)
		if summary == "" {
			summary = "—"
		}
		summary = ansi.Truncate(summary, subjectWidth, "…")
		row := ticketTableRow(marker, id, summary, stamp, idWidth, subjectWidth, timeWidth)
		if i == m.ticketCursor {
			row = selectedRowStyle().Render(row)
		} else {
			row = paint(row, browserValue, false)
		}
		lines = append(lines, paint(glyph, statusColor, true)+row)
	}
	if m.ticketState.status == requestStale {
		lines = append(lines, staleRequestTime("Tickets", m.ticketState))
	}
	return fitPaneLines(lines, height)
}

func (m *browser) ticketConversationLines(height int) []string {
	lines := []string{}
	if m.ticketCompose == ticketComposeDiscard {
		return fitPaneLines([]string{"Discard this draft? Enter yes · Esc keep editing"}, height)
	}
	if m.ticketCompose == ticketComposeNew {
		return m.ticketNewFormLines(height)
	}
	if m.ticketCompose == ticketComposeReview {
		return m.ticketReviewLines(height)
	}
	if m.ticketCompose != "" {
		lines = append(lines, strings.Split(m.ticketDraft+"█", "\n")...)
		if m.ticketNotice != "" {
			lines = append(lines, paint(m.ticketNotice, browserNumber, false))
		}
		_, detailWidth, _ := m.paneWidths()
		return fitPaneLines(alignTicketRTL(lines, detailWidth-ticketDetailHorizontalInset), height)
	}
	if m.ticketDetail == nil {
		if m.ticketDetailState.active() {
			return fitPaneLines([]string{m.loadingMessage("Loading conversation…")}, height)
		}
		return fitPaneLines([]string{"Select a ticket"}, height)
	}
	t := m.ticketDetail
	glyph, statusColor := ticketStatusGlyph(t.Status)
	category := t.RequestType.Value
	if category == "" {
		category = t.RequestType.Key
	}
	lines = append(lines, paint("#"+safeText(string(t.ID)), browserAccent, true)+"  "+paint(glyph, statusColor,
		true)+"  "+paint(safeText(category), browserMuted, false), paint(safeText(t.Summary), browserValue, true), "")
	_, detailWidth, _ := m.paneWidths()
	contentWidth := max(ticketMinimumContentWidth, detailWidth-ticketBodyVerticalInset)
	for _, c := range m.ticketComments {
		message := []string{ticketCommentHeader(c, contentWidth)}
		body := renderTicketBody(c.Body)
		for _, bodyLine := range body {
			wrapped := strings.Split(ansi.Wrap(bodyLine, contentWidth, ""), "\n")
			message = append(message, wrapped...)
		}
		for _, attachment := range c.Attachments {
			message = append(message, paint("↳ "+safeText(attachment.Filename), browserKey, false))
		}
		for _, line := range message {
			lines = append(lines, ticketMessageForeground(line, c.Author.IsStaff()))
		}
		lines = append(lines, "")
	}
	if m.ticketDetailState.err != nil {
		lines = append(lines, paint("Conversation could not be refreshed", browserDanger, false))
	}
	m.ticketScroll = min(max(0, m.ticketScroll), max(0, len(lines)-height))
	lines = lines[m.ticketScroll:]
	return fitPaneLines(lines, height)
}

func (m *browser) ticketChoiceLabels() (string, string) {
	category := m.ticketCategory
	for _, choice := range m.ticketCategories {
		if choice.Key == category && choice.Value != "" {
			category = choice.Value
			break
		}
	}
	if category == "" {
		category = "Loading categories…"
	}
	priority := "Platform default"
	if m.ticketPriority != "" {
		priority = "Unknown priority"
	}
	for _, choice := range m.ticketPriorities {
		if choice.ID == m.ticketPriority {
			priority = choice.Keyword
			break
		}
	}
	return safeText(category), safeText(priority)
}

func ticketPriorityColor(label string) color.Color {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case statusCritical:
		return browserDanger
	case "high":
		return browserNumber
	case "normal":
		return browserSuccess
	default:
		return browserMuted
	}
}

func (m *browser) ticketNewFormLines(height int) []string {
	category, priority := m.ticketChoiceLabels()
	field := func(index int, label, value string, valueColor color.Color) string {
		marker, labelColor := "  ", browserMuted
		if m.ticketComposeField == index {
			marker, labelColor = "› ", browserAccent
		}
		return paint(marker+label, labelColor, true) + "  " + paint(safeText(value), valueColor, false)
	}
	subject := m.ticketSummary
	if m.ticketComposeField == ticketSubjectFieldIndex {
		subject += "█"
	}
	if subject == "" {
		subject = "—"
	}
	description := m.ticketDraft
	if m.ticketComposeField == ticketMessageFieldIndex {
		description += "█"
	}
	if description == "" {
		description = "—"
	}
	lines := []string{paint("FILE A SUPPORT TICKET", browserAccent, true), paint("Tenant  "+safeText(m.tenant), browserMuted, false), "",
		field(0, "Category", category, browserValue), field(1, "Priority", priority, ticketPriorityColor(priority)), "",
		field(ticketSubjectFieldIndex, "Subject", subject, browserValue), "", field(ticketMessageFieldIndex, "Message", "",
			browserValue)}
	_, width, _ := m.paneWidths()
	for line := range strings.SplitSeq(description, "\n") {
		lines = append(lines, alignTicketRTL([]string{"  " + safeText(line)}, max(1, width-ticketFormHorizontalInset))...)
	}
	if m.ticketNotice != "" {
		lines = append(lines, "", paint(safeText(m.ticketNotice), browserNumber, false))
	}
	return fitPaneLines(lines, height)
}

func (m *browser) ticketReviewLines(height int) []string {
	category, priority := m.ticketChoiceLabels()
	lines := []string{paint("REVIEW YOUR TICKET", browserAccent, true), "", paint("Tenant", browserMuted,
		false) + "    " + safeText(m.tenant), paint("Category", browserMuted, false) + "  " + category, paint("Priority", browserMuted,
		false) + "  " + paint(priority, ticketPriorityColor(priority), true), "", paint("Subject", browserKey, true),
		safeText(m.ticketSummary), "", paint("Message", browserKey, true)}
	_, width, _ := m.paneWidths()
	for line := range strings.SplitSeq(m.ticketDraft, "\n") {
		lines = append(lines, alignTicketRTL([]string{safeText(line)}, max(1, width-ticketFormHorizontalInset))...)
	}
	if m.ticketNotice != "" {
		lines = append(lines, "", paint(safeText(m.ticketNotice), browserNumber, false))
	}
	return fitPaneLines(lines, height)
}

func ticketStatusGlyph(status string) (string, color.Color) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "open":
		return "●", browserSuccess
	case "close", "closed":
		return "✓", browserNumber
	default:
		return "?", browserNumber
	}
}

func ticketScopeControl(mine bool) string {
	if mine {
		return "Mine"
	}
	return "Org"
}

func ticketStatusControl(status string) string {
	if status == "" {
		status = client.TicketStatusAll
	}
	return status
}

func ticketControlSeparatorWidth(width int, status string, mine bool) int {
	available := max(1, width-borderTitleWithControlsValue6)
	label := ansi.Truncate("[3] TICKETS", available, "…")
	room := max(0, available-ansi.StringWidth(label)-1)
	full := ticketStatusControl(status) + "  " + ticketScopeControl(mine)
	if ansi.StringWidth(full) > room-2 {
		return 1
	}
	return ticketControlWideSeparator
}

func ticketVisibleRange(count, cursor, height int) (int, int) {
	capacity := max(1, height-ticketBodyVerticalInset)
	first := max(0, min(cursor-capacity+1, count-capacity))
	return first, min(count, first+capacity)
}

func ticketColumnWidths(width int) (int, int, int) {
	idWidth, timeWidth := 7, 16
	if width < ticketListMinimumWidth {
		timeWidth = 10
	}
	return idWidth, max(1, width-idWidth-timeWidth-ticketListColumnGap), timeWidth
}

func ticketTableRow(marker, id, subject, updated string, idWidth, subjectWidth, timeWidth int) string {
	pad := func(value string, width int) string {
		value = ansi.Truncate(safeText(value), width, "…")
		return value + strings.Repeat(" ", max(0, width-ansi.StringWidth(value)))
	}
	return marker + " " + pad(id, idWidth) + " " + pad(subject, subjectWidth) + " " + pad(updated, timeWidth)
}

func ticketMessageForeground(line string, staff bool) string {
	roleColor := browserSuccess
	if staff {
		roleColor = browserAccent
	}
	rolePrefix := strings.TrimSuffix(paint(" ", roleColor, false), " ")
	line = strings.ReplaceAll(line, "\x1b[0m", "\x1b[0m"+rolePrefix)
	return paint(line, roleColor, false)
}

func ticketTimestamp(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "—"
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999-0700", "2006-01-02T15:04:05-0700"} {
		if value, err := time.Parse(layout, raw); err == nil {
			return formatLocalWallTime(value, "2006-01-02 15:04")
		}
	}
	return safeText(raw)
}

func ticketCommentHeader(comment client.TicketComment, width int) string {
	author := strings.TrimSpace(comment.Author.DisplayName)
	if author == "" {
		author = strings.TrimSpace(comment.Author.Name)
	}
	if author == "" {
		author = strings.TrimSpace(comment.Author.Email)
	}
	if author == "" {
		author = "Unknown"
	}
	glyph, roleColor := "◆", browserSuccess
	if comment.Author.IsStaff() {
		glyph, roleColor = "●", browserAccent
	}
	timeText := ticketTimestamp(string(comment.Created))
	label := paint(glyph, roleColor, true)
	stamp := ansi.Truncate(timeText, max(1, width-ansi.StringWidth(label)-ticketTimestampRightInset), "…")
	nameWidth := max(0, width-ansi.StringWidth(label)-ansi.StringWidth(stamp)-ticketNameColumnGap)
	name := ""
	if nameWidth > 0 {
		name = " " + paint(ansi.Truncate(safeText(author), nameWidth, "…"), roleColor, true)
	}
	left := label + name
	return left + strings.Repeat(" ", max(1, width-ansi.StringWidth(left)-ansi.StringWidth(stamp))) + paint(stamp, browserMuted, false)
}

func alignTicketRTL(lines []string, width int) []string {
	aligned := make([]string, 0, len(lines))
	for _, line := range lines {
		plain := ansi.Strip(line)
		rtl, code := false, strings.HasPrefix(plain, "│")
		for _, r := range plain {
			if r >= 0x0590 && r <= 0x08ff {
				rtl = true
				break
			}
		}
		if rtl && !code {
			line = strings.Repeat(" ", max(0, width-ansi.StringWidth(line))) + line
		}
		aligned = append(aligned, line)
	}
	return aligned
}

func renderTicketBody(body string) []string {
	lines := strings.Split(body, "\n")
	inCode := false
	for i, line := range lines {
		line = safeText(line)
		lines[i] = line
		trimmed := strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(trimmed, "```"); ok {
			lines[i], inCode = ticketCodeFenceLine(after, inCode)
			continue
		}
		if inCode {
			lines[i] = paint("│ "+line, browserBranch, false)
			continue
		}
		lines[i] = renderInlineTicketCode(line)
	}
	return lines
}

func ticketCodeFenceLine(label string, inCode bool) (string, bool) {
	if inCode {
		return paint("└ code", browserMuted, false), false
	}
	if label != "" {
		return paint(label, browserMuted, true), true
	}
	return paint("┌ code", browserMuted, false), true
}

func renderInlineTicketCode(line string) string {
	parts := strings.Split(line, "`")
	if len(parts) == 1 {
		return line
	}
	for i := 1; i < len(parts); i += 2 {
		parts[i] = paint(parts[i], browserBranch, true)
	}
	return strings.Join(parts, "")
}

func (m *browser) ticketFooterLine(left string) string {
	hints := []keyHint{{"↑↓", "select"}, {hintEnter, "conversation"}, {"n", "new ticket"}, {"/", hintFilter}, {"f", "status"}, {"o",
		"mine/org"}, {"R", "refresh"}}
	switch m.ticketCompose {
	case ticketComposeReply:
		hints = []keyHint{{hintEnter, "send"}, {hintCtrlJ, hintNewline}, {hintEscape, hintCancel}}
	case ticketComposeNew:
		hints = []keyHint{{hintTab, "field"}, {"←→", "choose"}, {hintEnter, "next/review"}, {hintCtrlJ, "newline"}, {hintEscape, "cancel"}}
	case ticketComposeReview:
		hints = []keyHint{{hintEnter, "create"}, {hintEscape, "back"}}
	case "":
		if m.detail {
			hints = []keyHint{{"r", ticketComposeReply}, {"↑↓/PgUp/PgDn", hintScroll}, {hintEscape, "list"}, {"R", "refresh"}}
		}
	default:
		hints = []keyHint{{hintCtrlJ, "newline"}, {hintEnter, "send/review"}, {hintEscape, "cancel"}}
	}
	return m.rightFooterLine(left, m.fitFooterHints(left, hints))
}

func (m *browser) handleTicketKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	if m.ticketCompose == hintFilter {
		return m.handleTicketFilterKey(msg, k)
	}
	if m.ticketCompose != "" {
		return m.handleTicketComposeKey(msg, k)
	}
	if model, cmd, handled := m.handleTicketNavigationKey(k); handled {
		return model, cmd
	}
	return m.handleTicketListActionKey(k)
}

func (m *browser) handleTicketNavigationKey(key string) (tea.Model, tea.Cmd, bool) {
	if len(key) == 1 && key[0] >= '1' && key[0] <= '4' {
		return m, m.focusPane(int(key[0] - '1')), true
	}
	if key == "up" || key == "k" {
		model, cmd := m.handleTicketMovement(-1)
		return model, cmd, true
	}
	if key == "down" || key == "j" {
		model, cmd := m.handleTicketMovement(1)
		return model, cmd, true
	}
	if key == inputKeyPgUp || key == inputKeyPgDown {
		page := max(1, m.height-m.browserBodyY()-browserFooterHeight-ticketPageBottomInset)
		if key == inputKeyPgUp {
			page = -page
		}
		model, cmd := m.handleTicketMovement(page)
		return model, cmd, true
	}
	if key == browserKeyEscape {
		return m.handleTicketEscape(), nil, true
	}
	if key == keyEnter {
		model, cmd := m.handleTicketEnter()
		return model, cmd, true
	}
	if key == "r" {
		return m.handleTicketReplyKey(), nil, true
	}
	return nil, nil, false
}

func (m *browser) handleTicketListActionKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q", inputKeyCtrlC:
		return m, tea.Quit
	case "?":
		m.help = true
		return m, nil
	case inputKeyTab:
		return m, m.cycleFocus(false)
	case inputKeyShiftTab, inputKeyBacktab:
		return m, m.cycleFocus(true)
	case "n":
		m.ticketCompose = ticketComposeNew
		m.detail = true
		m.ticketComposeField = 0
		m.ticketDraft = ""
		m.ticketSummary = ""
		m.ticketCategory = ""
		m.ticketPriority = ""
		return m, m.loadTicketChoices()
	case "f":
		return m, m.cycleTicketStatus()
	case "o":
		m.ticketMine = !m.ticketMine
		m.ticketCursor = 0
		return m, m.loadTickets(true)
	case "R":
		m.ticketDetailState.reset()
		return m, tea.Batch(m.loadTickets(true), m.loadTicketThread())
	case "/":
		m.ticketFilter = ""
		m.ticketCompose = hintFilter
		return m, nil
	}
	return m, nil
}

func (m *browser) handleTicketMovement(delta int) (tea.Model, tea.Cmd) {
	if m.detail {
		m.ticketScroll = max(0, m.ticketScroll+delta)
		return m, nil
	}
	m.ticketCursor = min(max(0, m.ticketCursor+delta), max(0, len(m.visibleTickets())-1))
	return m, m.loadTicketThread()
}

func (m *browser) handleTicketEscape() tea.Model {
	if m.detail {
		m.detail = false
		return m
	}
	m.page = pageApps
	return m
}

func (m *browser) handleTicketEnter() (tea.Model, tea.Cmd) {
	if m.detail {
		return m, nil
	}
	m.detail = true
	return m, m.loadTicketThread()
}

func (m *browser) handleTicketReplyKey() tea.Model {
	if !m.detail {
		return m
	}
	m.ticketCompose, m.ticketDraft = ticketComposeReply, ""
	return m
}

func (m *browser) cycleTicketStatus() tea.Cmd {
	switch m.ticketStatus {
	case client.TicketStatusOpen:
		m.ticketStatus = client.TicketStatusClosed
	case client.TicketStatusClosed:
		m.ticketStatus = client.TicketStatusAll
	default:
		m.ticketStatus = client.TicketStatusOpen
	}
	m.ticketCursor = 0
	return m.loadTickets(true)
}

func (m *browser) handleTicketComposeKey(msg tea.KeyPressMsg, key string) (tea.Model, tea.Cmd) {
	if m.ticketSubmitting {
		return m, nil
	}
	if m.ticketCompose == ticketComposeDiscard {
		return m.handleTicketDiscardKey(key)
	}
	switch key {
	case browserKeyEscape:
		return m.handleTicketComposeEscape()
	case "ctrl+j":
		m.ticketDraft += "\n"
		return m, nil
	case inputKeyTab:
		if m.ticketCompose == ticketComposeNew {
			m.ticketComposeField = (m.ticketComposeField + 1) % ticketComposeFieldCount
		}
		return m, nil
	case inputKeyShiftTab, inputKeyBacktab:
		if m.ticketCompose == ticketComposeNew {
			m.ticketComposeField = (m.ticketComposeField + ticketLastComposeFieldIndex) % ticketComposeFieldCount
		}
		return m, nil
	case keyEnter:
		return m.handleTicketComposeEnter()
	case "backspace":
		return m.handleTicketComposeBackspace()
	case keyLeft, "right":
		if m.ticketCompose == ticketComposeNew && m.ticketComposeField == 0 {
			m.cycleTicketCategory(key)
		}
		if m.ticketCompose == ticketComposeNew && m.ticketComposeField == 1 {
			m.cycleTicketPriority(key)
		}
		return m, nil
	}
	m.appendTicketComposeText(msg.Text)
	return m, nil
}

func (m *browser) handleTicketDiscardKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case keyEnter:
		m.ticketCompose, m.ticketDraft, m.ticketSummary, m.ticketNotice = "", "", "", ""
	case browserKeyEscape:
		m.ticketCompose = m.ticketComposeBeforeDiscard
	}
	return m, nil
}

func (m *browser) handleTicketComposeEscape() (tea.Model, tea.Cmd) {
	if m.ticketCompose == ticketComposeReview {
		m.ticketCompose = ticketComposeNew
		return m, nil
	}
	if strings.TrimSpace(m.ticketDraft) == "" && strings.TrimSpace(m.ticketSummary) == "" {
		m.ticketCompose, m.ticketDraft, m.ticketNotice = "", "", ""
		return m, nil
	}
	m.ticketComposeBeforeDiscard = m.ticketCompose
	m.ticketCompose = ticketComposeDiscard
	return m, nil
}

func (m *browser) handleTicketComposeEnter() (tea.Model, tea.Cmd) {
	switch m.ticketCompose {
	case ticketComposeReply:
		return m, m.submitTicketReply()
	case ticketComposeNew:
		if m.ticketComposeField < ticketLastComposeFieldIndex {
			m.ticketComposeField++
			return m, nil
		}
		m.ticketCompose = ticketComposeReview
	case ticketComposeReview:
		return m, m.submitNewTicket()
	}
	return m, nil
}

func (m *browser) handleTicketComposeBackspace() (tea.Model, tea.Cmd) {
	if m.ticketCompose == ticketComposeNew && m.ticketComposeField == 2 {
		m.ticketSummary = dropLastRune(m.ticketSummary)
	} else {
		m.ticketDraft = dropLastRune(m.ticketDraft)
	}
	return m, nil
}

func (m *browser) cycleTicketCategory(key string) {
	if m.ticketCompose != ticketComposeNew || m.ticketComposeField != 0 || len(m.ticketCategories) == 0 {
		return
	}
	index := 0
	for i, category := range m.ticketCategories {
		if category.Key == m.ticketCategory {
			index = i
		}
	}
	if key == keyLeft {
		index = (index + len(m.ticketCategories) - 1) % len(m.ticketCategories)
	} else {
		index = (index + 1) % len(m.ticketCategories)
	}
	m.ticketCategory = m.ticketCategories[index].Key
}

func (m *browser) cycleTicketPriority(key string) {
	if m.ticketCompose != ticketComposeNew || len(m.ticketPriorities) == 0 {
		return
	}
	index := 0
	for i, priority := range m.ticketPriorities {
		if priority.ID == m.ticketPriority {
			index = i + 1
		}
	}
	count := len(m.ticketPriorities) + 1
	if key == keyLeft {
		index = (index + count - 1) % count
	} else {
		index = (index + 1) % count
	}
	m.ticketPriority = ""
	if index > 0 {
		m.ticketPriority = m.ticketPriorities[index-1].ID
	}
}

func (m *browser) appendTicketComposeText(raw string) {
	if raw == "" {
		return
	}
	text := safeText(raw)
	switch m.ticketCompose {
	case ticketComposeReply:
		m.ticketDraft += text
	case ticketComposeNew:
		switch m.ticketComposeField {
		case ticketLastComposeFieldIndex:
			m.ticketDraft += text
		case ticketSubjectFieldIndex:
			m.ticketSummary += text
		}
	}
}

func dropLastRune(value string) string {
	runes := []rune(value)
	if len(runes) == 0 {
		return value
	}
	return string(runes[:len(runes)-1])
}

func (m *browser) handleTicketFilterKey(msg tea.KeyPressMsg, key string) (tea.Model, tea.Cmd) {
	switch key {
	case keyEnter, browserKeyEscape:
		m.ticketCompose = ""
	case "backspace":
		m.ticketFilter = dropLastRune(m.ticketFilter)
	default:
		if msg.Text != "" {
			m.ticketFilter += safeText(msg.Text)
		}
	}
	return m, nil
}

func (m *browser) loadTicketChoices() tea.Cmd {
	tenant, generation, cats, prs := m.tenant, m.tenantGeneration, m.getTicketCategories, m.getTicketPriorities
	ctx := m.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return func() tea.Msg {
		if cats == nil {
			return ticketCategoriesResult{tenant: tenant, generation: generation, err: errTicketCategoriesUnavailable}
		}
		c, e := cats(ctx, tenant)
		if e != nil {
			return ticketCategoriesResult{tenant: tenant, generation: generation, err: e}
		}
		var p []client.TicketPriority
		if prs != nil {
			p, _ = prs(ctx, tenant)
		}
		return ticketCategoriesResult{tenant: tenant, generation: generation, categories: c, priorities: p}
	}
}
func (m *browser) submitTicketReply() tea.Cmd {
	tenant, generation, id, body, fn := m.tenant, m.tenantGeneration, m.ticketSelection, m.ticketDraft, m.replyTicket
	if strings.TrimSpace(body) == "" {
		m.ticketNotice = "Reply cannot be empty"
		return nil
	}
	if fn == nil {
		m.ticketNotice = "Reply is unavailable"
		return nil
	}
	m.ticketSubmitting = true
	return func() tea.Msg {
		ctx := m.ctx
		if ctx == nil {
			ctx = context.Background()
		}
		err := fn(ctx, tenant, id, body)
		return ticketWriteResult{tenant: tenant, generation: generation, id: id, err: err}
	}
}
func (m *browser) submitNewTicket() tea.Cmd {
	tenant, generation, summary, desc, cat, priority, fn := m.tenant, m.tenantGeneration, m.ticketSummary, m.ticketDraft,
		m.ticketCategory, m.ticketPriority, m.createTicket
	if strings.TrimSpace(summary) == "" || strings.TrimSpace(desc) == "" || cat == "" {
		m.ticketNotice = "Category, subject and description are required"
		return nil
	}
	if fn == nil {
		m.ticketNotice = "Ticket creation is unavailable"
		return nil
	}
	m.ticketSubmitting = true
	return func() tea.Msg {
		ctx := m.ctx
		if ctx == nil {
			ctx = context.Background()
		}
		id, err := fn(ctx, tenant, client.NewTicket{RequestType: cat, Summary: summary, Description: desc, Priority: priority})
		return ticketWriteResult{tenant: tenant, generation: generation, id: id, created: true, err: err}
	}
}

func (m *browser) handleTicketResult(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch r := msg.(type) {
	case ticketListResult:
		return m.handleTicketListResult(r)
	case ticketThreadResult:
		return m.handleTicketThreadResult(r)
	case ticketCategoriesResult:
		return m.handleTicketCategoriesResult(r)
	case ticketWriteResult:
		return m.handleTicketWriteResult(r)
	}
	return nil, nil, false
}

func (m *browser) handleTicketListResult(result ticketListResult) (tea.Model, tea.Cmd, bool) {
	m.finishRequest(requestTickets, result.meta)
	if !m.ticketState.matches(result.meta) || result.meta.tenant != m.tenant || result.meta.scopeGeneration != m.tenantGeneration {
		return m, nil, true
	}
	if result.err != nil {
		m.ticketState.fail(result.err)
		return m, nil, true
	}
	m.ticketRows = append([]client.Ticket(nil), result.rows.Tickets...)
	m.ticketState.succeed(len(m.ticketRows) == 0, m.requestTime())
	if m.ticketCursor >= len(m.visibleTickets()) {
		m.ticketCursor = max(0, len(m.visibleTickets())-1)
	}
	if len(m.visibleTickets()) > 0 && !m.detail {
		return m, m.loadTicketThread(), true
	}
	return m, nil, true
}

func (m *browser) handleTicketThreadResult(result ticketThreadResult) (tea.Model, tea.Cmd, bool) {
	m.finishRequest(requestTicketThread, result.meta)
	if !m.ticketDetailState.matches(result.meta) || result.meta.tenant != m.tenant ||
		result.meta.scopeGeneration != m.tenantGeneration || result.meta.appID != m.ticketSelection {
		return m, nil, true
	}
	if result.err != nil {
		m.ticketDetailState.fail(result.err)
		return m, nil, true
	}
	m.ticketDetail = &result.ticket
	m.ticketComments = append([]client.TicketComment(nil), result.comments...)
	m.ticketDetailState.succeed(false, m.requestTime())
	return m, nil, true
}

func (m *browser) handleTicketCategoriesResult(result ticketCategoriesResult) (tea.Model, tea.Cmd, bool) {
	if result.tenant != m.tenant || result.generation != m.tenantGeneration {
		return m, nil, true
	}
	if result.err != nil {
		m.ticketNotice = "Could not load ticket categories"
		return m, nil, true
	}
	m.ticketCategories = result.categories
	m.ticketPriorities = result.priorities
	if len(result.categories) > 0 {
		m.ticketCategory = result.categories[0].Key
	}
	return m, nil, true
}

func (m *browser) handleTicketWriteResult(result ticketWriteResult) (tea.Model, tea.Cmd, bool) {
	if result.tenant != m.tenant || result.generation != m.tenantGeneration {
		return m, nil, true
	}
	m.ticketSubmitting = false
	if result.err != nil {
		m.ticketNotice = "Submission failed; draft kept. Refresh before retry."
		if result.id != "" {
			m.ticketNotice = "Ticket #" + result.id + " created; follow-up failed. Draft kept."
		}
		return m, nil, true
	}
	if result.created {
		m.ticketNotice = "Ticket #" + result.id + " submitted"
		m.ticketCompose, m.ticketDraft, m.ticketCursor, m.detail = "", "", 0, false
		m.ticketStatus = client.TicketStatusAll
		return m, m.loadTickets(true), true
	}
	m.ticketNotice = "Reply sent"
	m.ticketCompose, m.ticketDraft = "", ""
	m.ticketDetailState.reset()
	m.ticketDetail = nil
	return m, m.loadTicketThread(), true
}
