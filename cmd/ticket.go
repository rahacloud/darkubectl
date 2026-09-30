package cmd

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/rahacloud/darkubectl/internal/output"
	"github.com/urfave/cli/v3"
)

// Flags of the ticket commands.
const (
	flagStatus      = "status"
	flagOrgWide     = "org-wide"
	flagSummary     = "summary"
	flagDescription = "description"
	flagMessage     = "message"
	flagPriority    = "priority"
	flagAttach      = "attach"
	flagComment     = "comment"
	flagDest        = "dest"

	argTicketID = "TICKET"
	// statusAll is --status's everything value.
	statusAll = "all"
	// attachmentPerm keeps a downloaded attachment private: it may hold a
	// customer's logs or credentials.
	attachmentPerm = 0o600
	// stdinArg is the conventional "read it from stdin" argument.
	stdinArg = "-"
	// maxStars is the top of the console's five-star rating.
	maxStars = 5
	// ticketStatusCol is the STATUS column of the ticket table.
	ticketStatusCol = 1
)

var (
	errMissingTicketID  = errors.New("a TICKET id argument is required")
	errTicketStatus     = errors.New("--status must be open, closed or all")
	errTicketType       = errors.New("--type is required: `darkubectl ticket types` lists the keys")
	errTicketSummary    = errors.New("--summary is required")
	errTicketBody       = errors.New("the ticket needs a description: --description, --file, or --file - for stdin")
	errReplyBody        = errors.New("the reply is empty: pass --message, --file, or --file - for stdin, or --attach a file")
	errStdinNeedsYes    = errors.New("reading the text from stdin leaves no way to confirm: add --yes")
	errRateUsage        = errors.New("usage: darkubectl ticket rate TICKET STARS (1-5)")
	errMissingAttachID  = errors.New("an ATTACHMENT id argument is required; `ticket view` lists them")
	errUnknownPriority  = errors.New("unknown priority")
	errUnknownTicketCat = errors.New("unknown ticket type")
)

// newTicketCommand is support ticketing — the console's پشتیبانی page. It is a
// command group rather than kubectl verbs because a ticket is a conversation, not
// a resource: `gh issue` is the closer model.
func newTicketCommand() *cli.Command {
	return &cli.Command{
		Name:    "ticket",
		Aliases: []string{"tickets", "support"},
		Usage:   "File, follow and answer Hamravesh support tickets",
		Description: "Tickets belong to the active tenant: select one with --org or `config use-tenant`.\n" +
			"Replies from Hamravesh staff are marked in `ticket view`.",
		Commands: []*cli.Command{
			newTicketListCommand("list", []string{"ls"}),
			{
				Name:      "view",
				Aliases:   []string{"show", "describe"},
				Usage:     "Show a ticket and its conversation",
				ArgsUsage: argTicketID,
				Action:    ticketViewAction,
			},
			newTicketCreateCommand(),
			newTicketReplyCommand(),
			{
				Name:      "close",
				Usage:     "Close a ticket",
				ArgsUsage: argTicketID,
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: flagYes, Aliases: []string{aliasYes}, Usage: usageSkipConfirm},
				},
				Action: ticketCloseAction,
			},
			{
				Name:      "rate",
				Usage:     "Rate the support on a ticket, 1 to 5 stars",
				ArgsUsage: argTicketID + " STARS",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: flagComment, Aliases: []string{"c"}, Usage: "a comment to go with the rating"},
				},
				Action: ticketRateAction,
			},
			{
				Name:      "download",
				Usage:     "Download a ticket attachment",
				ArgsUsage: "ATTACHMENT",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: flagDest, Usage: "where to write it: a file, a directory, or - for stdout (default: its own name here)"},
				},
				Action: ticketDownloadAction,
			},
			{
				Name:   "types",
				Usage:  "List the request types a ticket can be filed under",
				Action: ticketTypesAction,
			},
			{
				Name:   "priorities",
				Usage:  "List the priorities a ticket can be filed with",
				Action: ticketPrioritiesAction,
			},
		},
	}
}

// newTicketListCommand builds the list, shared by `ticket list` and `get tickets`.
func newTicketListCommand(name string, aliases []string) *cli.Command {
	return &cli.Command{
		Name:    name,
		Aliases: aliases,
		Usage:   "List support tickets",
		Description: "Lists the tickets this account filed in the active tenant; --org-wide lists\n" +
			"every member's.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagStatus, Value: statusAll, Usage: "open, closed or all"},
			&cli.StringFlag{Name: flagType, Usage: "only this request type (`ticket types` lists them)"},
			&cli.BoolFlag{Name: flagOrgWide, Usage: "every ticket in the organization, not just this account's"},
		},
		Action: ticketListAction,
	}
}

func ticketListAction(ctx context.Context, cmd *cli.Command) error {
	status, err := ticketStatusFilter(cmd.String(flagStatus))
	if err != nil {
		return err
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	format, err := outputFormat(cmd)
	if err != nil {
		return err
	}
	list, err := c.Tickets(ctx, status, cmd.String(flagType), !cmd.Bool(flagOrgWide))
	if err != nil {
		return err
	}

	if handled, err := output.Structured(os.Stdout, format, list.Tickets); handled {
		return err
	}
	if format == output.Name {
		for _, t := range list.Tickets {
			fmt.Fprintln(os.Stdout, t.ID)
		}
		return nil
	}
	if len(list.Tickets) == 0 {
		fmt.Fprintln(os.Stderr, "no tickets")
		return nil
	}

	header := []string{"ID", colStatus, colType, "CREATED", "UPDATED", "SUMMARY"}
	if format == output.Wide {
		header = append(header, "REPORTER")
	}
	rows := make([][]string, 0, len(list.Tickets))
	for _, t := range list.Tickets {
		row := []string{
			string(t.ID), t.Status, dash(t.RequestType.Key),
			shortTime(t.Created), shortTime(t.Updated), oneLine(t.Summary),
		}
		if format == output.Wide {
			row = append(row, dash(t.Reporter.Email))
		}
		rows = append(rows, row)
	}
	return output.StyledTable(os.Stdout, header, rows, ticketCells)
}

// ticketCells draws attention to open tickets and mutes closed ones.
func ticketCells(col int, value string) color.Color {
	if col != ticketStatusCol {
		return nil
	}
	if strings.EqualFold(value, client.TicketStatusOpen) {
		return output.ColorNumber
	}
	return output.ColorMuted
}

// ticketStatusFilter maps --status onto the list route's enum.
func ticketStatusFilter(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", statusAll:
		return "", nil
	case "open":
		return client.TicketStatusOpen, nil
	case "closed", "close":
		return client.TicketStatusClosed, nil
	default:
		return "", errTicketStatus
	}
}

// ticketView is what `ticket view -o json|yaml` prints.
type ticketView struct {
	Ticket   client.Ticket          `json:"ticket"`
	Comments []client.TicketComment `json:"comments"`
}

func ticketViewAction(ctx context.Context, cmd *cli.Command) error {
	id := cmd.Args().First()
	if id == "" {
		return errMissingTicketID
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	format, err := outputFormat(cmd)
	if err != nil {
		return err
	}
	t, err := c.Ticket(ctx, id)
	if err != nil {
		return err
	}
	comments, err := c.TicketComments(ctx, id)
	if err != nil {
		return err
	}

	if handled, err := output.Structured(os.Stdout, format, ticketView{Ticket: t, Comments: comments}); handled {
		return err
	}
	if format == output.Name {
		fmt.Fprintln(os.Stdout, t.ID)
		return nil
	}
	return renderTicket(os.Stdout, t, comments)
}

// renderTicket prints a ticket as a header followed by its thread. The first
// comment is the description itself, so the header does not repeat it.
func renderTicket(w io.Writer, t client.Ticket, comments []client.TicketComment) error {
	if err := output.PrintSectionHeader(w, fmt.Sprintf("#%s  %s", t.ID, strings.TrimSpace(t.Summary))); err != nil {
		return err
	}
	category := t.RequestType.Key
	if t.RequestType.Value != "" && t.RequestType.Value != category {
		category += " (" + t.RequestType.Value + ")"
	}
	fmt.Fprintf(w, "Status:    %s\n", t.Status)
	fmt.Fprintf(w, "Type:      %s\n", dash(category))
	fmt.Fprintf(w, "Reporter:  %s\n", dash(t.Reporter.Email))
	fmt.Fprintf(w, "Created:   %s\n", shortTime(t.Created))
	if t.Rate > 0 {
		fmt.Fprintf(w, "Rating:    %s\n", strings.Repeat("*", t.Rate)+ratingComment(t.RateComment))
	}

	for _, cm := range comments {
		who := dash(cm.Author.DisplayName)
		if cm.Author.IsStaff() {
			who += " [Hamravesh]"
		}
		fmt.Fprintln(w)
		if err := output.PrintSectionHeader(w, "--- "+who+"  "+shortTime(string(cm.Created))); err != nil {
			return err
		}
		fmt.Fprintln(w, strings.TrimSpace(cm.Body))
		for _, a := range cm.Attachments {
			fmt.Fprintf(w, "  attachment %s: %s\n", a.ID, a.Filename)
		}
	}
	if hidden := t.CommentCount - len(comments); hidden > 0 {
		fmt.Fprintf(w, "\n(%d internal comment(s) not shown)\n", hidden)
	}
	return nil
}

func ratingComment(c string) string {
	if strings.TrimSpace(c) == "" {
		return ""
	}
	return "  " + oneLine(c)
}

func newTicketCreateCommand() *cli.Command {
	return &cli.Command{
		Name:  "create",
		Usage: "File a support ticket",
		Description: "  darkubectl ticket create --type darkube --summary 'Pod stuck in Pending' \\\n" +
			"      --description 'app foo in namespace bar has been Pending since 10:00'\n" +
			"  darkubectl ticket create --type financial -s 'Invoice question' -f question.md --attach invoice.pdf\n\n" +
			"`ticket types` lists the request types and `ticket priorities` the priorities.\n" +
			"The description is Markdown.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagType, Aliases: []string{"t"}, Usage: "request type key (`ticket types`)"},
			&cli.StringFlag{Name: flagSummary, Aliases: []string{"s"}, Usage: "one-line title"},
			&cli.StringFlag{Name: flagDescription, Aliases: []string{"d"}, Usage: "the description (Markdown)"},
			&cli.StringFlag{Name: flagFile, Aliases: []string{"f"}, Usage: "read the description from a file (- for stdin)"},
			&cli.StringFlag{Name: flagPriority, Aliases: []string{"p"}, Usage: "normal, high or critical (default: the platform's)"},
			&cli.StringSliceFlag{Name: flagAttach, Aliases: []string{"a"}, Usage: "attach a file (repeatable)"},
			&cli.BoolFlag{Name: flagYes, Aliases: []string{aliasYes}, Usage: usageSkipConfirm},
		},
		Action: ticketCreateAction,
	}
}

func ticketCreateAction(ctx context.Context, cmd *cli.Command) error {
	category := strings.TrimSpace(cmd.String(flagType))
	if category == "" {
		return errTicketType
	}
	summary := strings.TrimSpace(cmd.String(flagSummary))
	if summary == "" {
		return errTicketSummary
	}
	body, fromStdin, err := ticketText(cmd.String(flagDescription), cmd.String(flagFile))
	if err != nil {
		return err
	}
	if strings.TrimSpace(body) == "" {
		return errTicketBody
	}
	if fromStdin && !cmd.Bool(flagYes) {
		return errStdinNeedsYes
	}
	files, err := ticketFiles(cmd.StringSlice(flagAttach))
	if err != nil {
		return err
	}

	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	if err := checkTicketCategory(ctx, c, category); err != nil {
		return err
	}
	priority, err := resolveTicketPriority(ctx, c, cmd.String(flagPriority))
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "About to file a %s ticket with Hamravesh support for tenant %q:\n  %s\n",
		category, c.Org, summary)
	for _, f := range files {
		fmt.Fprintf(os.Stderr, "  + %s\n", f.Path)
	}
	if !cmd.Bool(flagYes) && !confirm() {
		return errAborted
	}

	id, err := c.CreateTicket(ctx, client.NewTicket{
		RequestType: category,
		Summary:     summary,
		Description: body,
		Priority:    priority,
		Attachments: files,
	})
	if id != "" {
		fmt.Fprintf(os.Stdout, "ticket/%s created\n", id)
	}
	return err
}

func newTicketReplyCommand() *cli.Command {
	return &cli.Command{
		Name:      "reply",
		Aliases:   []string{"comment"},
		Usage:     "Reply to a ticket",
		ArgsUsage: argTicketID,
		Description: "  darkubectl ticket reply 33421 -m 'Thanks, that fixed it.'\n" +
			"  darkubectl ticket reply 33421 -f notes.md --attach trace.log",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagMessage, Aliases: []string{"m"}, Usage: "the reply (Markdown)"},
			&cli.StringFlag{Name: flagFile, Aliases: []string{"f"}, Usage: "read the reply from a file (- for stdin)"},
			&cli.StringSliceFlag{Name: flagAttach, Aliases: []string{"a"}, Usage: "attach a file (repeatable)"},
		},
		Action: ticketReplyAction,
	}
}

// ticketReplyAction sends without a prompt, as `gh issue comment` does: the
// command line is the whole message, already read back by whoever typed it.
func ticketReplyAction(ctx context.Context, cmd *cli.Command) error {
	id := cmd.Args().First()
	if id == "" {
		return errMissingTicketID
	}
	body, _, err := ticketText(cmd.String(flagMessage), cmd.String(flagFile))
	if err != nil {
		return err
	}
	files, err := ticketFiles(cmd.StringSlice(flagAttach))
	if err != nil {
		return err
	}
	if strings.TrimSpace(body) == "" && len(files) == 0 {
		return errReplyBody
	}

	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	if err := c.ReplyToTicket(ctx, id, body, files); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "ticket/%s replied\n", id)
	return nil
}

func ticketCloseAction(ctx context.Context, cmd *cli.Command) error {
	id := cmd.Args().First()
	if id == "" {
		return errMissingTicketID
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	t, err := c.Ticket(ctx, id)
	if err != nil {
		return err
	}
	if !t.IsOpen() {
		fmt.Fprintf(os.Stdout, "ticket/%s is already %s\n", id, strings.ToLower(t.Status))
		return nil
	}
	fmt.Fprintf(os.Stderr, "About to close ticket #%s: %s\n", id, oneLine(t.Summary))
	if !cmd.Bool(flagYes) && !confirm() {
		return errAborted
	}
	if err := c.CloseTicket(ctx, id); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "ticket/%s closed\n", id)
	return nil
}

func ticketRateAction(ctx context.Context, cmd *cli.Command) error {
	if cmd.Args().Len() != 2 { //nolint:mnd // TICKET and STARS
		return errRateUsage
	}
	id := cmd.Args().Get(0)
	stars, err := strconv.Atoi(cmd.Args().Get(1))
	if err != nil || stars < 1 || stars > maxStars {
		return errRateUsage
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	if err := c.RateTicket(ctx, id, stars, cmd.String(flagComment)); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "ticket/%s rated %d/%d\n", id, stars, maxStars)
	return nil
}

func ticketDownloadAction(ctx context.Context, cmd *cli.Command) error {
	id := cmd.Args().First()
	if id == "" {
		return errMissingAttachID
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	name, data, err := c.DownloadTicketAttachment(ctx, id)
	if err != nil {
		return err
	}
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = "attachment-" + id
	}

	dest := cmd.String(flagDest)
	if dest == stdinArg {
		_, err := os.Stdout.Write(data)
		return err
	}
	switch {
	case dest == "":
		dest = name
	case isDir(dest):
		dest = filepath.Join(dest, name)
	}
	if err := os.WriteFile(dest, data, attachmentPerm); err != nil {
		return fmt.Errorf("write %s: %w", dest, err)
	}
	fmt.Fprintf(os.Stderr, "%s (%d bytes)\n", dest, len(data))
	return nil
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func ticketTypesAction(ctx context.Context, cmd *cli.Command) error {
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	format, err := outputFormat(cmd)
	if err != nil {
		return err
	}
	cats, err := c.TicketCategories(ctx)
	if err != nil {
		return err
	}
	if handled, err := output.Structured(os.Stdout, format, cats); handled {
		return err
	}
	rows := make([][]string, 0, len(cats))
	for _, t := range cats {
		if format == output.Name {
			fmt.Fprintln(os.Stdout, t.Key)
			continue
		}
		rows = append(rows, []string{t.Key, t.Value})
	}
	if format == output.Name {
		return nil
	}
	return output.StyledTable(os.Stdout, []string{colType, colName}, rows, nil)
}

func ticketPrioritiesAction(ctx context.Context, cmd *cli.Command) error {
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	format, err := outputFormat(cmd)
	if err != nil {
		return err
	}
	prios, err := c.TicketPriorities(ctx)
	if err != nil {
		return err
	}
	if handled, err := output.Structured(os.Stdout, format, prios); handled {
		return err
	}
	rows := make([][]string, 0, len(prios))
	for _, p := range prios {
		if format == output.Name {
			fmt.Fprintln(os.Stdout, strings.ToLower(p.Keyword))
			continue
		}
		rows = append(rows, []string{strings.ToLower(p.Keyword), p.ID})
	}
	if format == output.Name {
		return nil
	}
	return output.StyledTable(os.Stdout, []string{"PRIORITY", "ID"}, rows, nil)
}

// ticketText resolves a message given inline or as a file, where the file "-"
// is stdin. It reports whether stdin was used, since that rules out a prompt.
func ticketText(inline, file string) (string, bool, error) {
	switch {
	case file == "":
		return inline, false, nil
	case inline != "":
		return "", false, fmt.Errorf("give the text inline or with --%s, not both", flagFile)
	case file == stdinArg:
		b, err := io.ReadAll(os.Stdin)
		return string(b), true, err
	default:
		b, err := os.ReadFile(file) //nolint:gosec // the user's own message file
		if err != nil {
			return "", false, fmt.Errorf("read %s: %w", file, err)
		}
		return string(b), false, nil
	}
}

// ticketFiles checks each attachment exists and is a regular file before
// anything is sent, so a typo does not leave a ticket filed without its files.
func ticketFiles(paths []string) ([]client.TicketFile, error) {
	files := make([]client.TicketFile, 0, len(paths))
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return nil, fmt.Errorf("attach %s: %w", p, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("attach %s: not a regular file", p)
		}
		files = append(files, client.TicketFile{Path: p})
	}
	return files, nil
}

// checkTicketCategory rejects an unknown request type before the create, which
// would otherwise fail with a bare validation error from Jira.
func checkTicketCategory(ctx context.Context, c *client.Client, key string) error {
	cats, err := c.TicketCategories(ctx)
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(cats))
	for _, t := range cats {
		if t.Key == key {
			return nil
		}
		keys = append(keys, t.Key)
	}
	return fmt.Errorf("%w %q: one of %s", errUnknownTicketCat, key, strings.Join(keys, ", "))
}

// resolveTicketPriority maps a keyword (normal, high, critical) to the id the
// create body wants. An empty keyword leaves the platform's default.
func resolveTicketPriority(ctx context.Context, c *client.Client, keyword string) (string, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return "", nil
	}
	prios, err := c.TicketPriorities(ctx)
	if err != nil {
		return "", err
	}
	names := make([]string, 0, len(prios))
	for _, p := range prios {
		if strings.EqualFold(p.Keyword, keyword) || p.ID == keyword {
			return p.ID, nil
		}
		names = append(names, strings.ToLower(p.Keyword))
	}
	return "", fmt.Errorf("%w %q: one of %s", errUnknownPriority, keyword, strings.Join(names, ", "))
}
