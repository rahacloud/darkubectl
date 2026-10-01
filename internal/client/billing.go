package client

import (
	"context"
	"net/url"
	"strconv"
)

// Billing, from the console's financial pages (console-financial, confirmed
// 2026-10-01, reads only). Both are scoped to the organization named by
// X-Organization. Amounts are plain numbers in the platform's currency; the
// console prints them unconverted, with the unit taken from its translations.
//
//	GET /api/v1/billing/wallets/                 bare list: {id,budget,type,priority,billing_account}
//	GET /api/v1/billing/invoices/?limit=&offset= DRF list, newest first
const (
	walletsPath  = "/api/v1/billing/wallets/"
	invoicesPath = "/api/v1/billing/invoices/"
)

// Wallet is one balance an organization pays from: "cash" or "gift" credit.
// Priority is the order they are drawn down in.
type Wallet struct {
	ID       string  `json:"id"       yaml:"id"`
	Type     string  `json:"type"     yaml:"type"`
	Budget   float64 `json:"budget"   yaml:"budget"`
	Priority int     `json:"priority" yaml:"priority"`
}

// Invoice is one monthly bill. The current month's is "pending" and grows
// until its end_time.
type Invoice struct {
	ID            string  `json:"id"             yaml:"id"`
	Number        int64   `json:"number"         yaml:"number"`
	StartTime     string  `json:"start_time"     yaml:"start_time"`
	EndTime       string  `json:"end_time"       yaml:"end_time"`
	PaymentStatus string  `json:"payment_status" yaml:"payment_status"`
	OrdersPrice   float64 `json:"orders_price"   yaml:"orders_price"`
	DiscountPrice float64 `json:"discount_price" yaml:"discount_price"`
	TaxPrice      float64 `json:"tax_price"      yaml:"tax_price"`
	FinalPrice    float64 `json:"final_price"    yaml:"final_price"`
	Payable       float64 `json:"payable_amount" yaml:"payable_amount"`
}

// Wallets returns the organization's balances.
func (c *Client) Wallets(ctx context.Context) ([]Wallet, error) {
	var out []Wallet
	if err := c.getJSON(ctx, walletsPath, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Invoices returns the most recent invoices, newest first, up to limit.
func (c *Client) Invoices(ctx context.Context, limit int) ([]Invoice, error) {
	q := url.Values{}
	q.Set("limit", strconv.Itoa(limit))
	q.Set("offset", "0")
	var p page[Invoice]
	if err := c.getJSON(ctx, invoicesPath, q, &p); err != nil {
		return nil, err
	}
	return p.Results, nil
}
