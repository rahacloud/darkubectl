package client

import (
	"context"
	"net/url"
	"strconv"
)

// notificationsPathV1 is the account's notification feed. It is paginated like
// the rest of the v1 surface but is not scoped to a tenant: the feed spans every
// organization the account belongs to.
const notificationsPathV1 = "/api/v1/notifications/all_list/"

// alertsPath is the monitoring alert feed.
//
// Note the prefix: it is served from /notifications/alerts/api/, outside the
// /api/v1/ tree everything else lives under, and it answers with a bare JSON
// array rather than the usual DRF {count,results} envelope.
const alertsPath = "/notifications/alerts/api/alerts/"

// Notification is one entry of the account's notification feed. Titles and
// descriptions are Persian prose, and the description carries HTML.
type Notification struct {
	Slug        int    `json:"slug"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Timestamp   string `json:"timestamp"`
	TargetType  string `json:"target_type"`
	ActionURL   string `json:"action_url"`
	ActionLabel string `json:"action_label"`
	Unread      bool   `json:"unread"`
	Public      bool   `json:"public"`
}

// Alert is one monitoring alert raised against the tenant's resources.
type Alert struct {
	ID               string `json:"id"`
	AlertName        string `json:"alertname"`
	Status           string `json:"status"`
	Severity         string `json:"severity"`
	Instance         string `json:"instance"`
	ServiceOwner     string `json:"service_owner"`
	Condition        string `json:"condition"`
	AlertDescription string `json:"alert_description"`
	DescriptionFA    string `json:"description_fa"`
	TimeWindow       string `json:"timewindow"`
	StartsAt         string `json:"starts_at"`
	EndsAt           string `json:"ends_at"`
	ActionLink       string `json:"action_link"`
	ForMe            bool   `json:"for_me"`
}

// IsFiring reports whether the alert is currently active rather than resolved.
func (a Alert) IsFiring() bool { return a.Status != "resolved" }

// Notifications returns the most recent notifications, newest first, following
// pagination up to limit entries.
func (c *Client) Notifications(ctx context.Context, limit int) ([]Notification, error) {
	const pageSize = 50
	var all []Notification
	offset := 0
	for len(all) < limit {
		want := min(pageSize, limit-len(all))

		q := url.Values{}
		q.Set("limit", strconv.Itoa(want))
		q.Set("offset", strconv.Itoa(offset))

		var p page[Notification]
		if err := c.getJSON(ctx, notificationsPathV1, q, &p); err != nil {
			return nil, err
		}
		all = append(all, p.Results...)
		offset += want
		if p.Next == "" || len(p.Results) == 0 || len(all) >= p.Count {
			break
		}
	}
	return all, nil
}

// Alerts returns the tenant's monitoring alerts, both firing and resolved.
func (c *Client) Alerts(ctx context.Context) ([]Alert, error) {
	var out []Alert
	if err := c.getJSON(ctx, alertsPath, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
