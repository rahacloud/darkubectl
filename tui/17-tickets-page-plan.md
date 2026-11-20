# Tickets page plan

Status: implementation in progress. All examples are fictional. This plan supersedes the global read-only rule in `00-start-here.md` **only for ticket creation and replies**: these actions are available without `--allow-writes`. It does not change MCP policy or enable other TUI mutations.

## Workspace preview

Keep the existing tenant and Pages rail; activate Tickets like DB. At a wide terminal, show a ticket list and the selected conversation together. English shell labels follow the current TUI; ticket content retains its original language.

```text
 Darkubectl / team-a / Tickets                                      MOCK
╭─ [1] TENANTS ─╮ ╭─ [3] TICKETS ───────────────╮ ╭─ [4] CONVERSATION ─────────────────────╮
│ › team-a      │ │ Mine · Open     / filter    │ │ #33421  مشکل اتصال به دیتابیس         │
├─ [2] PAGES ───┤ │                             │ │ ● Open · Database · 2026-11-20         │
│ ▦ Apps        │ │ › #33421  ● Open            │ │                                        │
│ ∿ Activity    │ │   مشکل اتصال به دیتابیس     │ │ You · 09:30                           │
│ ▱ Tickets     │ │   09:30                     │ │ اتصال به پایگاه داده برقرار نمی‌شود.  │
│ ▤ DB          │ │                             │ │ خطا: connection refused on port 5432   │
│ ◇ Registry    │ │   #33408  ✓ Closed          │ │                                        │
│               │ │   پیگیری صورتحساب           │ │ Support · 10:12                       │
│               │ │                             │ │ لطفاً نام پروژه و زمان خطا را بفرستید. │
│               │ │                             │ │                                        │
│               │ │                             │ │ r reply · ↑/↓ scroll                    │
╰───────────────╯ ╰─────────────────────────────╯ ╰────────────────────────────────────────╯
 1/2 rail · 3 list · 4 conversation   n new · / filter · f status · R refresh · ? help
```

`r` is reply only in the conversation; use `R` for refresh throughout Tickets, avoiding ambiguity. Exact key bindings must be checked against the current global router and help before implementation. List rows show ID, status, title and available timestamp; do not label the API's comment count as a reliable reply count. Sort by available updated time, falling back to created time, then ID; display unknown values as `—`. Default to Mine / All, with status Open / Closed / All and Mine / Organization filters; `f` cycles through the status choices and the active choice stays visible in the list header. Request the API with its required `request_type=all`. Search locally by ID and title. Empty, no matches, loading, failed and stale states remain distinct. Show server counts only with their actual all-status meaning.

Use `● Open` and `✓ Closed` consistently in list and detail, with text as well as glyph and color; unknown statuses use `?` and their raw value. Normalize the API's `Close` to the display label `Closed`. The mock above illustrates both states; All is the initial list filter.

At narrow widths, show list or conversation in place; Enter/4 opens the selected conversation, Esc returns to list. Preserve selection by ticket ID within a tenant. Scroll the thread independently and do not clip essential controls. The description is the first comment (ID 0); do not render it twice. Render author and timestamp, mark support based on the client's existing staff-domain rule, and do not imply that missing internal comments are visible. Show attachment filenames when returned; attachment upload and download remain CLI workflows in this first slice.

## Reply and new ticket previews

```text
╭─ Reply · team-a / #33421 ─────────────────────────────────────────────╮
│ اتصال همچنان قطع است.                                               │
│ pod: api-7d8f · 10:30                                                │
│                                                                      │
│ Ctrl+J newline · Enter send · Esc cancel                              │
╰──────────────────────────────────────────────────────────────────────╯
```

```text
╭─ New ticket · team-a ─────────────────────────────────────────────────╮
│ Category   [ Database ▾ ]    Priority [ Platform default ▾ ]          │
│ Subject    مشکل اتصال به دیتابیس                                      │
│                                                                      │
│ Description                                                          │
│ از ساعت ۱۰ اتصال برقرار نمی‌شود.                                      │
│                                                                      │
│ Tab next field · Ctrl+J newline · Enter review                        │
╰──────────────────────────────────────────────────────────────────────╯
```

```text
╭─ Review new ticket ───────────────────────────────────────────────────╮
│ Tenant: team-a  ·  Category: Database  ·  Priority: Platform default  │
│ Subject: مشکل اتصال به دیتابیس                                       │
│ Description: از ساعت ۱۰ اتصال برقرار نمی‌شود.                        │
│                                                                      │
│ Enter create · Esc back to draft                                      │
╰──────────────────────────────────────────────────────────────────────╯
```

New ticket requires category, nonblank subject and description; fetch categories and optional priorities from the client and use their actual keys/IDs. A category loading error must not masquerade as an empty list. In a reply, Enter sends and Ctrl+J inserts a newline. In the new-ticket form, Enter advances to the complete scrollable review; Enter on review creates the ticket, with tenant and category visible. In selectors, Enter selects the highlighted choice. Verify terminal key decoding: some terminals send identical bytes for Enter and Ctrl+J, so provide a clearly documented alternate newline binding where they cannot be distinguished; never silently send when a newline was intended. Retain drafts across validation failures and failed requests; ask before discarding a nonempty draft. Disable duplicate submits while pending. After success reload the relevant list/thread and retain the target. If a request times out or the response is ambiguous, keep the draft and state that the server may have accepted it; offer refresh before any manual retry, never retry automatically. If create returns an ID alongside an error, show the ID and partial outcome rather than offering an unconditional second create.

Capture tenant, ticket ID and request generation at submission; switching tenant or selection cannot redirect a pending operation or put its result into another scope. Clearly distinguish submission from subsequent refresh failure. Closed ticket reply behavior is not assumed: expose the server error and preserve the draft if rejected. Close and rate actions are outside this slice.

## Code in conversations

Support Markdown inline code delimited by single backticks and fenced code blocks delimited by triple backticks in message display. Preserve the original message and submit it unchanged. Render inline code distinctly and code blocks as left-to-right, preformatted text with whitespace and line breaks preserved, an optional language label, and horizontal scrolling or an explicit overflow indicator for long lines. Treat content as text, never execute it or interpret terminal escape sequences. Unclosed or malformed delimiters remain readable as literal text. Syntax highlighting is optional; legibility and correct mixed Persian/code direction take precedence. Exercise backticks, fences, indentation, long lines, mixed RTL prose and resizing in mock fixtures and terminal checks.

## Persian, RTL, and mixed text

- Treat direction per paragraph, not per page: detect Persian/Arabic strong characters; allow an explicit direction override for ambiguous or mixed paragraphs. Keep IDs, dates, URLs, paths and code in their original logical LTR order. Align RTL prose to the right and wrap by terminal cells and grapheme clusters, preserving punctuation, digits, ZWNJ, combining marks, newlines and original bytes for submission. Never reverse a string or store visually reordered text.
- Terminal bidi and Arabic shaping vary substantially. Before choosing a rendering strategy, prototype logical Unicode rendering in the project's actual supported terminals and assess whether the terminal itself shapes/reorders text. A display-only bidi/shaping solution must avoid double processing in terminals that already do it; do not claim universal RTL support. Document the tested terminals and a selectable fallback if necessary. Isolate untrusted direction/control characters so ticket content cannot affect surrounding UI or emit terminal control sequences.
- Editing must preserve logical text and support Persian keyboard input, paste, multiline text, ZWNJ and combining marks. Verify cursor movement, deletion and selection at grapheme boundaries in both directions. If the existing editor cannot reliably support visual cursor movement with mixed-direction text, use a tested editor component or an external-editor handoff and verify the exact saved text; do not ship a misleading RTL editor.
- Validate with real terminal rendering, not only snapshots: Persian-only, English-only and mixed Persian/English, URLs, numbers, parentheses, Markdown, long wraps and resize. Verify exact submitted text with a fake backend and test both mock and live adapters without logging message contents.

## Data, state and offline mock

Use typed adapters around `Client.Tickets`, `Ticket`, `TicketComments`, `TicketCategories`, `TicketPriorities`, `CreateTicket` and `ReplyToTicket`; do not shell out. List, detail/thread and category/priority loads have separate request states. Cancel or invalidate on tenant change, selection change and refresh, including A → B → A; never show a previous tenant's thread while the next one loads. Keep stale same-scope data visible with a retry hint on refresh failure. Sanitize terminal output and errors without changing submitted text.

Extend the existing YAML mock profile with tenant-scoped synthetic tickets, thread messages, categories and priorities. Supply Persian, mixed-direction and English examples, including an empty tenant and a closed ticket. Create and reply update an in-memory mock store so the entire flow can be previewed offline without credentials, network calls or `--allow-writes`; clearly mark MOCK and make changes session-only. Check old mock profiles remain compatible and missing fixtures produce sensible empty states.

## Delivery and acceptance

1. Prototype and verify RTL display and editing across supported terminals; settle direction and editor behavior before committing to the widget. Record actual limitations.
2. Add tenant-scoped list/thread reads and offline fixture with wide, narrow, loading, empty, error and stale views. Test scope invalidation, counts and selection.
3. Add new ticket and reply flows, explicitly exempting these two actions from the TUI write gate. Verify all other writes and MCP authorization remain unchanged. Test validation, target binding, duplicate prevention, failures, ambiguous outcomes and refresh.
4. Exercise complete workflows in mock mode, terminal resize and keyboard/mouse navigation; run focused tests and repository-required build, race tests and lint. Separately report manual terminal evidence and any live verification; do not send real tickets as a test without an explicit request.
