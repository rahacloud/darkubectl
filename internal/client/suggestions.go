package client

import (
	"context"
	"net/url"
)

// Suggestion is a piece of configuration advice the platform derives from an
// app, such as a port the image exposes that the app does not. Title and Text
// are Persian prose.
type Suggestion struct {
	ID       string `json:"id"              yaml:"id"`
	Type     string `json:"suggestion_type" yaml:"suggestion_type"`
	Severity string `json:"severity"        yaml:"severity"`
	Title    string `json:"title"           yaml:"title"`
	Text     string `json:"text"            yaml:"text"`
	Docs     string `json:"docs_address"    yaml:"docs_address"`
}

// AppSuggestions computes and returns the platform's advice for an app. The
// route is a POST (GET answers 405) with an empty body, as the console calls
// it; it records the suggestions it returns. Confirmed 2026-10-01.
func (c *Client) AppSuggestions(ctx context.Context, appID string) ([]Suggestion, error) {
	var out []Suggestion
	if err := c.postJSON(ctx, appsPathV1+url.PathEscape(appID)+"/suggestions/", map[string]any{}, &out); err != nil {
		return nil, err
	}
	return out, nil
}
