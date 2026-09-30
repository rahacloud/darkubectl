package client_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rahacloud/darkubectl/internal/client"
)

// The list and the detail disagree on the id's type, the request type's shape
// and the reporter's email key; these are trimmed live responses.
const (
	ticketListBody = `{"message":"Operation successful","code":200,"data":{"total":4,"number_close":3,"number_open":1,` +
		`"tickets":[{"id":"33421","summary":"cluster access","description":"d","created":"2026-09-21T10:28:00.000+0000",` +
		`"updated":"2026-09-29T14:57:10.000+0000","reporter":{"name":"a@b.c","emailAddress":"a@b.c","displayName":"A"},` +
		`"status":"Open","request_type":"managed_kubernetes","rate":0,"rate_comment":null}]}}`
	ticketDetailBody = `{"message":"get was successful","code":200,"data":{"id":33421,"summary":"cluster access",` +
		`"description":"d","status":"Open","reporter":{"name":"a@b.c","displayName":"A","email":"a@b.c"},` +
		`"request_type":{"key":"managed_kubernetes","value":"Managed Kubernetes"},"comment_number":19,` +
		`"created":"2026-09-21T10:28:00.597+0000","rate":0,"rate_comment":null}}`
	ticketCommentsBody = `{"message":"ok","code":200,"data":{"total":3,"start":0,"limit":10,"values":[` +
		`{"id":0,"body":"d","public":true,"author":{"emailAddress":"a@b.c","displayName":"A"},` +
		`"created":{"iso8601":"2026-09-21T13:58:00+0330"},"attachments":[]},` +
		`{"id":331268,"body":"hi","public":true,"author":{"emailAddress":"eng@Hamravesh.com","displayName":"Eng"},` +
		`"created":{"iso8601":"2026-09-21T14:28:06+0330","epochMillis":1789988286715},` +
		`"attachments":[{"id":"9001","filename":"trace.log"}],"missing_attachments":0}]}}`
)

func TestTicketsDecodesListAndSendsFilters(t *testing.T) {
	t.Parallel()

	var gotQuery, gotOrg, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery, gotOrg = r.URL.Path, r.URL.RawQuery, r.Header.Get("X-Organization")
		_, _ = io.WriteString(w, ticketListBody)
	}))
	defer srv.Close()

	c := client.New(srv.URL, client.BearerToken("jwt"), "rahacloud")
	list, err := c.Tickets(context.Background(), client.TicketStatusOpen, "", false)
	if err != nil {
		t.Fatalf("Tickets: %v", err)
	}
	if gotPath != "/support-platform/ticket" || gotOrg != "rahacloud" {
		t.Errorf("path %q org %q", gotPath, gotOrg)
	}
	if gotQuery != "my_ticket=false&request_type=all&status=Open" {
		t.Errorf("query = %q", gotQuery)
	}
	if list.Total != 4 || list.Open != 1 || list.Closed != 3 || len(list.Tickets) != 1 {
		t.Fatalf("list = %+v", list)
	}
	tk := list.Tickets[0]
	if tk.ID != "33421" || tk.RequestType.Key != "managed_kubernetes" || tk.Reporter.Email != "a@b.c" || !tk.IsOpen() {
		t.Errorf("ticket = %+v", tk)
	}
}

func TestTicketDecodesDetailShape(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, ticketDetailBody)
	}))
	defer srv.Close()

	tk, err := client.New(srv.URL, client.BearerToken("jwt"), "o").Ticket(context.Background(), "33421")
	if err != nil {
		t.Fatalf("Ticket: %v", err)
	}
	if tk.ID != "33421" || tk.RequestType.Value != "Managed Kubernetes" || tk.Reporter.Email != "a@b.c" || tk.CommentCount != 19 {
		t.Errorf("ticket = %+v", tk)
	}
}

func TestTicketCommentsMarkStaffAndKeepAttachments(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, ticketCommentsBody)
	}))
	defer srv.Close()

	cs, err := client.New(srv.URL, client.BearerToken("jwt"), "o").TicketComments(context.Background(), "33421")
	if err != nil {
		t.Fatalf("TicketComments: %v", err)
	}
	if len(cs) != 2 {
		t.Fatalf("got %d comments", len(cs))
	}
	if cs[0].Author.IsStaff() || !cs[1].Author.IsStaff() {
		t.Errorf("staff = %v, %v; want false, true", cs[0].Author.IsStaff(), cs[1].Author.IsStaff())
	}
	if cs[1].Created != "2026-09-21T14:28:06+0330" {
		t.Errorf("created = %q", cs[1].Created)
	}
	if len(cs[1].Attachments) != 1 || cs[1].Attachments[0].ID != "9001" {
		t.Errorf("attachments = %+v", cs[1].Attachments)
	}
}

func TestSupportErrorsAreReadable(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		status int
		body   string
		want   string
	}{
		"envelope": {http.StatusUnprocessableEntity, `{"message":"Missing Organization","code":422,"data":{}}`, "Missing Organization"},
		"fastapi": {
			http.StatusUnprocessableEntity,
			`{"detail":[{"type":"enum","loc":["query","status"],"msg":"Input should be 'Closed', 'Open' or 'All'"}]}`,
			"query.status: Input should be 'Closed', 'Open' or 'All'",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()

			_, err := client.New(srv.URL, client.BearerToken("jwt"), "").TicketPriorities(context.Background())
			apiErr, ok := errors.AsType[*client.APIError](err)
			if !ok {
				t.Fatalf("err = %v, want *APIError", err)
			}
			if apiErr.Detail != tc.want || apiErr.StatusCode != tc.status {
				t.Errorf("detail = %q (%d), want %q", apiErr.Detail, apiErr.StatusCode, tc.want)
			}
		})
	}
}

// TestCreateTicketStagesThenLinksAttachments pins the console's two-step create:
// files are uploaded first, the create goes out with empty attachments, and an
// attachment-comment then links the staged files to the new ticket.
func TestCreateTicketStagesThenLinksAttachments(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "trace.log")
	if err := os.WriteFile(file, []byte("boom"), 0o600); err != nil {
		t.Fatal(err)
	}

	var (
		mu    sync.Mutex
		calls []string
		body  map[string]any
		link  map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/support-platform/ticket/attachment":
			f, h, err := r.FormFile("file")
			if err != nil || h.Filename != "trace.log" {
				t.Errorf("upload: %v %v", err, h)
			} else {
				_ = f.Close()
			}
			_, _ = io.WriteString(w, `{"message":"ok","code":200,"data":[{"temporaryAttachmentId":"tmp-1"}]}`)
		case "/support-platform/ticket":
			_ = json.NewDecoder(r.Body).Decode(&body)
			_, _ = io.WriteString(w, `{"message":"ok","code":200,"data":{"issueId":"40000"}}`)
		case "/support-platform/ticket/40000/attachment-comment":
			_ = json.NewDecoder(r.Body).Decode(&link)
			_, _ = io.WriteString(w, `{"message":"ok","code":200,"data":{}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	id, err := client.New(srv.URL, client.BearerToken("jwt"), "o").CreateTicket(context.Background(), client.NewTicket{
		RequestType: "darkube", Summary: "s", Description: "d", Priority: "3",
		Attachments: []client.TicketFile{{Path: file}},
	})
	if err != nil || id != "40000" {
		t.Fatalf("CreateTicket = %q, %v", id, err)
	}
	want := "POST /support-platform/ticket/attachment,POST /support-platform/ticket," +
		"POST /support-platform/ticket/40000/attachment-comment"
	if got := strings.Join(calls, ","); got != want {
		t.Errorf("calls = %s", got)
	}
	if body["request_type"] != "darkube" || body["priority"] != "3" || body["attachments"] == nil {
		t.Errorf("create body = %v", body)
	}
	staged, _ := link["attachments"].([]any)
	if len(staged) != 1 {
		t.Fatalf("link body = %v", link)
	}
	a, _ := staged[0].(map[string]any)
	if a["temporary_attachment_id"] != "tmp-1" || a["filename"] != "trace.log" || a["size"] != float64(4) {
		t.Errorf("staged = %v", a)
	}
}

func TestCloseTicketTransitions(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotMethod, gotPath, gotBody = r.Method, r.URL.Path, string(b)
		_, _ = io.WriteString(w, `{"message":"ok","code":200,"data":null}`)
	}))
	defer srv.Close()

	if err := client.New(srv.URL, client.BearerToken("jwt"), "o").CloseTicket(context.Background(), "33421"); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPut || gotPath != "/support-platform/ticket/33421/transition" ||
		strings.TrimSpace(gotBody) != `{"status":"close"}` {
		t.Errorf("%s %s %s", gotMethod, gotPath, gotBody)
	}
}

func TestDownloadTicketAttachmentUsesServerFilename(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/support-platform/download-file/9001" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Disposition", `attachment; filename="../trace.log"`)
		_, _ = io.WriteString(w, "boom")
	}))
	defer srv.Close()

	name, data, err := client.New(srv.URL, client.BearerToken("jwt"), "o").DownloadTicketAttachment(context.Background(), "9001")
	if err != nil {
		t.Fatal(err)
	}
	if name != "trace.log" || string(data) != "boom" {
		t.Errorf("name %q data %q", name, data)
	}
}
