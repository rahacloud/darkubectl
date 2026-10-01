package cmd

import (
	"context"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/rahacloud/darkubectl/internal/output"
	"github.com/urfave/cli/v3"
)

const (
	defaultInvoiceLimit = 6
	// invoiceStatusCol is the colored column of the invoice table.
	invoiceStatusCol = 2
)

func newGetBalanceCommand() *cli.Command {
	return &cli.Command{
		Name:    "balance",
		Aliases: []string{"wallet", "wallets", "credit"},
		Usage:   "Show the tenant's balance: its cash and gift wallets",
		Description: "Amounts are as the API gives them, in the platform's currency, which the\n" +
			"console also shows unconverted. Pair with `get invoices` to see what the\n" +
			"current month has cost so far.",
		Action: getBalanceAction,
	}
}

func newGetInvoicesCommand() *cli.Command {
	return &cli.Command{
		Name:    "invoices",
		Aliases: []string{"invoice", "bills"},
		Usage:   "List the tenant's monthly invoices, newest first",
		Description: "The current month's invoice is pending and grows until its period ends.\n" +
			"Amounts are as the API gives them, in the platform's currency.",
		Flags: []cli.Flag{
			&cli.IntFlag{Name: flagLimit, Value: defaultInvoiceLimit, Usage: "how many invoices to list"},
		},
		Action: getInvoicesAction,
	}
}

func getBalanceAction(ctx context.Context, cmd *cli.Command) error {
	format, err := outputFormat(cmd)
	if err != nil {
		return err
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	wallets, err := c.Wallets(ctx)
	if err != nil {
		return err
	}
	if handled, err := output.Structured(os.Stdout, format, wallets); handled {
		return err
	}
	var total float64
	rows := make([][]string, 0, len(wallets)+1)
	for _, w := range wallets {
		total += w.Budget
		rows = append(rows, []string{w.Type, amount(w.Budget), strconv.Itoa(w.Priority)})
	}
	rows = append(rows, []string{"total", amount(total), ""})
	return output.StyledTable(os.Stdout, []string{"WALLET", "BALANCE", "DRAWN IN ORDER"}, rows, nil)
}

func getInvoicesAction(ctx context.Context, cmd *cli.Command) error {
	format, err := outputFormat(cmd)
	if err != nil {
		return err
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	invoices, err := c.Invoices(ctx, cmd.Int(flagLimit))
	if err != nil {
		return err
	}
	if handled, err := output.Structured(os.Stdout, format, invoices); handled {
		return err
	}
	if len(invoices) == 0 {
		fmt.Fprintln(os.Stderr, "no invoices")
		return nil
	}
	rows := make([][]string, 0, len(invoices))
	for _, inv := range invoices {
		rows = append(rows, []string{
			strconv.FormatInt(inv.Number, 10), shortDate(inv.StartTime) + " – " + shortDate(inv.EndTime),
			dash(inv.PaymentStatus), amount(inv.OrdersPrice), amount(inv.TaxPrice), amount(inv.FinalPrice),
		})
	}
	return output.StyledTable(os.Stdout, []string{"NUMBER", "PERIOD", colStatus, "USAGE", "TAX", "TOTAL"}, rows,
		output.StateCells(invoiceStatusCol))
}

// amount renders a currency amount with thousands separators and no
// fraction; the API's amounts are whole units.
func amount(v float64) string {
	n := int64(math.Round(v))
	neg := n < 0
	if neg {
		n = -n
	}
	s := strconv.FormatInt(n, 10)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// shortDate is the date part of an RFC 3339 timestamp.
func shortDate(ts string) string {
	if d, _, ok := strings.Cut(ts, "T"); ok {
		return d
	}
	return dash(ts)
}
