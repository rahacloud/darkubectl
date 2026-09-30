package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// supportPrefix is the support platform: Hamravesh's ticketing, a FastAPI service
// in front of Jira Service Desk. It lives outside the /api/ tree entirely, and it
// speaks a different dialect from the Darkube API:
//
//   - every response is wrapped in {"message","code","data"}, and errors carry an
//     integer code in that same envelope, or FastAPI's {"detail":[…]} on 422;
//   - it is tenant-scoped, but a missing X-Organization is 422 "Missing
//     Organization" rather than 403;
//   - ids are Jira issue numbers, strings in the list and numbers in the detail.
//
// Mapped on 2026-09-30 from the console's own `console-support` bundle, whose
// source maps ship with it.
const supportPrefix = "/support-platform"

// StaffEmailDomain is how the console tells a support engineer's comment from a
// customer's: nothing in the comment says so, only the author's address.
const StaffEmailDomain = "@hamravesh.com"

// Ticket status filter values, exactly as the list route's enum spells them.
// Note the ticket's own status reads "Close", not "Closed".
const (
	TicketStatusAll    = "All"
	TicketStatusOpen   = "Open"
	TicketStatusClosed = "Closed"
)

// Errors for answers the support platform accepted without the id they owe.
var (
	ErrNoTicketID     = errors.New("support platform accepted the ticket but returned no id")
	ErrNoAttachmentID = errors.New("support platform accepted the upload but returned no attachment id")
)

// TicketID is a Jira issue number. The list sends it as a string and the detail
// as a number, so it decodes from either.
type TicketID string

// UnmarshalJSON accepts a JSON string or number.
func (t *TicketID) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*t = TicketID(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return fmt.Errorf("ticket id: %w", err)
	}
	*t = TicketID(n.String())
	return nil
}

// TicketRequestType is a ticket's category. The list sends only the key as a
// bare string; the detail sends {key, value} with a localized value.
type TicketRequestType struct {
	Key   string `json:"key"`
	Value string `json:"value,omitempty"`
}

// UnmarshalJSON accepts a bare key or a {key, value} object.
func (r *TicketRequestType) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*r = TicketRequestType{Key: s}
		return nil
	}
	type plain TicketRequestType
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return fmt.Errorf("request type: %w", err)
	}
	*r = TicketRequestType(p)
	return nil
}

// TicketUser is a ticket's reporter or a comment's author. Jira calls the address
// `emailAddress` and the support platform's own detail calls it `email`; both
// land in Email.
type TicketUser struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Email       string `json:"email"`
}

// UnmarshalJSON folds `emailAddress` into Email.
func (u *TicketUser) UnmarshalJSON(b []byte) error {
	var raw struct {
		Name         string `json:"name"`
		DisplayName  string `json:"displayName"`
		Email        string `json:"email"`
		EmailAddress string `json:"emailAddress"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return fmt.Errorf("ticket user: %w", err)
	}
	*u = TicketUser{Name: raw.Name, DisplayName: raw.DisplayName, Email: raw.Email}
	if u.Email == "" {
		u.Email = raw.EmailAddress
	}
	return nil
}

// IsStaff reports whether the user is a Hamravesh support engineer.
func (u TicketUser) IsStaff() bool {
	return strings.HasSuffix(strings.ToLower(u.Email), StaffEmailDomain)
}

// TicketTime is a comment timestamp. Jira sends {iso8601, jira, friendly,
// epochMillis}; only iso8601 is always present, so that is what is kept.
type TicketTime string

// UnmarshalJSON accepts a bare string or Jira's timestamp object.
func (t *TicketTime) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*t = TicketTime(s)
		return nil
	}
	var o struct {
		ISO8601 string `json:"iso8601"`
	}
	if err := json.Unmarshal(b, &o); err != nil {
		return fmt.Errorf("ticket time: %w", err)
	}
	*t = TicketTime(o.ISO8601)
	return nil
}

// Ticket is one support ticket. Created and Updated are Jira timestamps
// (`2026-09-26T08:25:33.000+0000`), which are not RFC 3339.
type Ticket struct {
	ID           TicketID          `json:"id"`
	Summary      string            `json:"summary"`
	Description  string            `json:"description"`
	Status       string            `json:"status"`
	RequestType  TicketRequestType `json:"request_type"`
	Reporter     TicketUser        `json:"reporter"`
	Created      string            `json:"created"`
	Updated      string            `json:"updated,omitempty"`
	CommentCount int               `json:"comment_number,omitempty"`
	Rate         int               `json:"rate"`
	RateComment  string            `json:"rate_comment,omitempty"`
}

// IsOpen reports whether the ticket is still open.
func (t Ticket) IsOpen() bool { return strings.EqualFold(t.Status, TicketStatusOpen) }

// TicketList is the list route's answer. Total counts every status, whatever the
// filter; Tickets holds only the matching ones.
type TicketList struct {
	Total   int      `json:"total"`
	Open    int      `json:"number_open"`
	Closed  int      `json:"number_close"`
	Tickets []Ticket `json:"tickets"`
}

// TicketAttachment is a file on a comment, fetched with DownloadTicketAttachment.
type TicketAttachment struct {
	ID       TicketID `json:"id"`
	Filename string   `json:"filename"`
}

// TicketComment is one entry of a ticket's thread. The first is synthesized from
// the ticket's description with id 0. Internal Jira comments are left out, so a
// ticket's CommentCount can exceed the number returned.
type TicketComment struct {
	ID          TicketID           `json:"id"`
	Body        string             `json:"body"`
	Public      bool               `json:"public"`
	Author      TicketUser         `json:"author"`
	Created     TicketTime         `json:"created"`
	Attachments []TicketAttachment `json:"attachments"`
}

// TicketCategory is a request type a ticket can be filed under.
type TicketCategory struct {
	ID    string `json:"id"`
	Key   string `json:"key"`
	Value string `json:"value"`
}

// TicketPriority is a priority a ticket can be filed with. Create takes the ID,
// not the keyword.
type TicketPriority struct {
	ID      string `json:"id"`
	Keyword string `json:"keyword"`
}

// TicketFile is a local file to attach to a ticket or reply.
type TicketFile struct {
	Path string
}

// supportEnvelope is how the support platform wraps every answer.
type supportEnvelope[T any] struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
	Data    T      `json:"data"`
}

// supportJSON sends one JSON request to the support platform and unwraps the
// envelope into out.
func supportJSON[T any](ctx context.Context, c *Client, method, path string, query url.Values, body any) (T, error) {
	var zero T
	data, err := c.do(ctx, method, supportPrefix+path, query, body)
	if err != nil {
		return zero, supportError(err)
	}
	var env supportEnvelope[T]
	if err := json.Unmarshal(data, &env); err != nil {
		return zero, fmt.Errorf("decode response: %w", err)
	}
	return env.Data, nil
}

// supportError rewrites the support platform's error bodies, which the DRF
// decoder in do cannot read, into a readable Detail: the envelope's message, or
// FastAPI's validation errors.
func supportError(err error) error {
	apiErr, ok := errors.AsType[*APIError](err)
	if !ok {
		return err
	}
	var body struct {
		Message string          `json:"message"`
		Detail  json.RawMessage `json:"detail"`
	}
	if json.Unmarshal([]byte(apiErr.Detail), &body) != nil {
		return err
	}
	if body.Message != "" {
		apiErr.Detail = body.Message
		return apiErr
	}
	var issues []struct {
		Loc []any  `json:"loc"`
		Msg string `json:"msg"`
	}
	if json.Unmarshal(body.Detail, &issues) == nil && len(issues) > 0 {
		parts := make([]string, 0, len(issues))
		for _, i := range issues {
			loc := make([]string, 0, len(i.Loc))
			for _, l := range i.Loc {
				loc = append(loc, fmt.Sprint(l))
			}
			parts = append(parts, strings.Join(loc, ".")+": "+i.Msg)
		}
		apiErr.Detail = strings.Join(parts, "; ")
		return apiErr
	}
	var detail string
	if json.Unmarshal(body.Detail, &detail) == nil && detail != "" {
		apiErr.Detail = detail
	}
	return apiErr
}

// Tickets lists the tenant's tickets. status is one of the TicketStatus
// constants ("" means all), requestType a category key ("" means all), and
// mine limits the list to tickets this account reported rather than every
// ticket in the organization.
func (c *Client) Tickets(ctx context.Context, status, requestType string, mine bool) (TicketList, error) {
	q := url.Values{}
	if status != "" {
		q.Set("status", status)
	}
	if requestType == "" {
		requestType = "all"
	}
	// request_type is required by the route even when nothing is filtered.
	q.Set("request_type", requestType)
	q.Set("my_ticket", strconv.FormatBool(mine))
	return supportJSON[TicketList](ctx, c, http.MethodGet, "/ticket", q, nil)
}

// Ticket returns one ticket.
func (c *Client) Ticket(ctx context.Context, id string) (Ticket, error) {
	return supportJSON[Ticket](ctx, c, http.MethodGet, "/ticket/"+url.PathEscape(id), nil, nil)
}

// TicketComments returns a ticket's public thread, oldest first, starting with
// the description. The route answers with {total,start,limit,values} but ignores
// start and limit, so there is nothing to page through.
func (c *Client) TicketComments(ctx context.Context, id string) ([]TicketComment, error) {
	page, err := supportJSON[struct {
		Values []TicketComment `json:"values"`
	}](ctx, c, http.MethodGet, "/ticket/"+url.PathEscape(id)+"/comment", nil, nil)
	if err != nil {
		return nil, err
	}
	return page.Values, nil
}

// TicketCategories returns the request types a ticket can be filed under.
func (c *Client) TicketCategories(ctx context.Context) ([]TicketCategory, error) {
	return supportJSON[[]TicketCategory](ctx, c, http.MethodGet, "/ticket/request-type", nil, nil)
}

// TicketPriorities returns the priorities a ticket can be filed with.
func (c *Client) TicketPriorities(ctx context.Context) ([]TicketPriority, error) {
	return supportJSON[[]TicketPriority](ctx, c, http.MethodGet, "/ticket/priorities", nil, nil)
}

// NewTicket is the body of a ticket create. Priority is a TicketPriority ID and
// may be empty, in which case the platform picks its default.
type NewTicket struct {
	RequestType string
	Summary     string
	Description string
	Priority    string
	Attachments []TicketFile
}

// CreateTicket files a ticket and attaches any files to it, returning its id.
//
// The console does this in two steps and so does this: the create body's own
// `attachments` is always sent empty, files are uploaded to a staging area first,
// and a separate attachment-comment then links them to the new ticket. A failure
// in that second step leaves a ticket without its files, which is reported as an
// error alongside the id.
func (c *Client) CreateTicket(ctx context.Context, t NewTicket) (string, error) {
	staged := make([]stagedAttachment, 0, len(t.Attachments))
	for _, f := range t.Attachments {
		s, err := c.stageTicketAttachment(ctx, f.Path)
		if err != nil {
			return "", err
		}
		staged = append(staged, s)
	}

	body := map[string]any{
		"request_type": t.RequestType,
		"summary":      t.Summary,
		"description":  t.Description,
		"fields":       []any{},
		"attachments":  []any{},
	}
	if t.Priority != "" {
		body["priority"] = t.Priority
	}
	created, err := supportJSON[struct {
		IssueID TicketID `json:"issueId"`
	}](ctx, c, http.MethodPost, "/ticket", nil, body)
	if err != nil {
		return "", err
	}
	id := string(created.IssueID)
	if id == "" {
		return "", ErrNoTicketID
	}

	if len(staged) > 0 {
		_, err := supportJSON[json.RawMessage](ctx, c, http.MethodPost,
			"/ticket/"+url.PathEscape(id)+"/attachment-comment", nil,
			map[string]any{"attachments": staged})
		if err != nil {
			return id, fmt.Errorf("ticket %s was created, but attaching files failed: %w", id, err)
		}
	}
	return id, nil
}

// ReplyToTicket adds a comment to a ticket, uploading any files to it first.
// Unlike create, a reply's files are uploaded against the ticket itself and then
// referenced by id in the comment.
func (c *Client) ReplyToTicket(ctx context.Context, id, body string, files []TicketFile) error {
	type ref struct {
		ID       json.Number `json:"id"`
		Filename string      `json:"filename"`
	}
	refs := make([]ref, 0, len(files))
	for _, f := range files {
		up, err := supportUpload[struct {
			ID json.Number `json:"id"`
		}](ctx, c, "/ticket/"+url.PathEscape(id)+"/attachment/comment", f.Path)
		if err != nil {
			return err
		}
		refs = append(refs, ref{ID: up.ID, Filename: filepath.Base(f.Path)})
	}

	payload := map[string]any{"body": body}
	if len(refs) > 0 {
		payload["attachments"] = refs
	}
	_, err := supportJSON[json.RawMessage](ctx, c, http.MethodPost, "/ticket/"+url.PathEscape(id)+"/comment", nil, payload)
	return err
}

// CloseTicket closes a ticket.
func (c *Client) CloseTicket(ctx context.Context, id string) error {
	_, err := supportJSON[json.RawMessage](ctx, c, http.MethodPut, "/ticket/"+url.PathEscape(id)+"/transition", nil,
		map[string]string{"status": "close"})
	return err
}

// RateTicket rates the support on a ticket, 1 to 5 stars, with an optional comment.
func (c *Client) RateTicket(ctx context.Context, id string, stars int, comment string) error {
	_, err := supportJSON[json.RawMessage](ctx, c, http.MethodPut, "/ticket/"+url.PathEscape(id)+"/rate", nil,
		map[string]any{"value": stars, "comment": comment})
	return err
}

// DownloadTicketAttachment fetches an attachment by id, returning the filename
// the server suggests (empty if it names none) and the file's bytes.
func (c *Client) DownloadTicketAttachment(ctx context.Context, attachmentID string) (string, []byte, error) {
	path := supportPrefix + "/download-file/" + url.PathEscape(attachmentID)
	resp, err := c.http.R().SetContext(ctx).SetHeader("Accept", "*/*").Get(path)
	if err != nil {
		return "", nil, fmt.Errorf("GET %s: %w", path, err)
	}
	data := resp.Bytes()
	if resp.IsStatusFailure() {
		detail := strings.TrimSpace(string(data))
		if detail == "" {
			detail = resp.Status()
		}
		return "", nil, supportError(&APIError{StatusCode: resp.StatusCode(), Detail: detail})
	}
	name := ""
	if _, params, err := mime.ParseMediaType(resp.Header().Get("Content-Disposition")); err == nil {
		name = filepath.Base(params["filename"])
	}
	return name, data, nil
}

// stagedAttachment is a file uploaded ahead of a ticket create, in the shape the
// attachment-comment route takes it back.
type stagedAttachment struct {
	TemporaryAttachmentID string `json:"temporary_attachment_id"`
	Filename              string `json:"filename"`
	MimeType              string `json:"mime_type"`
	Size                  int    `json:"size"`
}

func (c *Client) stageTicketAttachment(ctx context.Context, path string) (stagedAttachment, error) {
	up, err := supportUpload[[]struct {
		TemporaryAttachmentID string `json:"temporaryAttachmentId"`
	}](ctx, c, "/ticket/attachment", path)
	if err != nil {
		return stagedAttachment{}, err
	}
	if len(up) == 0 || up[0].TemporaryAttachmentID == "" {
		return stagedAttachment{}, fmt.Errorf("upload %s: %w", path, ErrNoAttachmentID)
	}
	info, err := os.Stat(path)
	if err != nil {
		return stagedAttachment{}, fmt.Errorf("stat %s: %w", path, err)
	}
	return stagedAttachment{
		TemporaryAttachmentID: up[0].TemporaryAttachmentID,
		Filename:              filepath.Base(path),
		MimeType:              attachmentMimeType(path),
		Size:                  int(info.Size()),
	}, nil
}

// supportUpload posts one file as multipart field "file" and unwraps the envelope.
func supportUpload[T any](ctx context.Context, c *Client, path, file string) (T, error) {
	var zero T
	data, err := os.ReadFile(file) //nolint:gosec // a file the user chose to attach
	if err != nil {
		return zero, fmt.Errorf("read %s: %w", file, err)
	}
	resp, err := c.http.R().SetContext(ctx).
		SetMultipartField("file", filepath.Base(file), attachmentMimeType(file), bytes.NewReader(data)).
		Post(supportPrefix + path)
	if err != nil {
		return zero, fmt.Errorf("upload %s: %w", file, err)
	}
	body := resp.Bytes()
	if resp.IsStatusFailure() {
		detail := strings.TrimSpace(string(body))
		if detail == "" {
			detail = resp.Status()
		}
		return zero, fmt.Errorf("upload %s: %w", file,
			supportError(&APIError{StatusCode: resp.StatusCode(), Detail: detail}))
	}
	var env supportEnvelope[T]
	if err := json.Unmarshal(body, &env); err != nil {
		return zero, fmt.Errorf("upload %s: decode response: %w", file, err)
	}
	return env.Data, nil
}

// attachmentMimeType guesses a file's type from its extension, as the browser
// does, falling back to the generic binary type the console uses.
func attachmentMimeType(path string) string {
	if t := mime.TypeByExtension(filepath.Ext(path)); t != "" {
		return t
	}
	return "application/octet-stream"
}
