package client

import (
	"context"
	"net/url"
	"time"
)

// historyPathV1 is the app's audit log over a time range, as the console's
// history tab reads it. The unranged apps/<id>/history/ answers with the same
// shape; this is the route the console uses, so it is the one that is kept up.
const historyPathV1 = "/api/v1/darkube/stateless_apps/"

// HistoryEntry is one write to an app: who made it, when, and the fields it
// changed.
type HistoryEntry struct {
	Date      string          `json:"history_date" yaml:"history_date"`
	Type      string          `json:"history_type" yaml:"history_type"`
	UserEmail string          `json:"user_email"   yaml:"user_email"`
	UserID    int             `json:"user_id"      yaml:"user_id"`
	Changes   []HistoryChange `json:"changes"      yaml:"changes"`
}

// HistoryChange is one field of a HistoryEntry. Old and New are the field's
// values JSON-encoded as strings, so a list reads back as "[\"a.example\"]".
type HistoryChange struct {
	Field string `json:"field" yaml:"field"`
	Old   string `json:"old"   yaml:"old"`
	New   string `json:"new"   yaml:"new"`
}

type historyEnvelope struct {
	Diffs []HistoryEntry `json:"diffs"`
}

// AppHistory returns the writes made to an app between since and until,
// newest first.
func (c *Client) AppHistory(ctx context.Context, id string, since, until time.Time) ([]HistoryEntry, error) {
	q := url.Values{}
	q.Set("start_time", since.UTC().Format(time.RFC3339))
	q.Set("end_time", until.UTC().Format(time.RFC3339))

	var env historyEnvelope
	if err := c.getJSON(ctx, historyPathV1+url.PathEscape(id)+"/history/", q, &env); err != nil {
		return nil, err
	}
	return env.Diffs, nil
}
