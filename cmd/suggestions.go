package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/rahacloud/darkubectl/internal/output"
	"github.com/urfave/cli/v3"
)

func newGetSuggestionsCommand() *cli.Command {
	return &cli.Command{
		Name:      "suggestions",
		Aliases:   []string{"suggestion", "advice"},
		Usage:     "Show the platform's configuration advice for an app",
		ArgsUsage: argRefUsage,
		Description: "The checks the console runs on an app, such as a port the image exposes that\n" +
			"the app does not declare. Titles and text are in Persian, as the API gives them.",
		Action: getSuggestionsAction,
	}
}

func getSuggestionsAction(ctx context.Context, cmd *cli.Command) error {
	format, err := outputFormat(cmd)
	if err != nil {
		return err
	}
	c, app, err := resolveAppArg(ctx, cmd)
	if err != nil {
		return err
	}
	items, err := c.AppSuggestions(ctx, app.ID)
	if err != nil {
		return err
	}
	if handled, err := output.Structured(os.Stdout, format, items); handled {
		return err
	}
	if len(items) == 0 {
		fmt.Fprintf(os.Stderr, "no suggestions for app/%s\n", app.Name)
		return nil
	}
	rows := make([][]string, 0, len(items))
	for _, s := range items {
		rows = append(rows, []string{dash(s.Severity), dash(s.Type), dash(s.Title), dash(s.Text)})
	}
	return output.StyledTable(os.Stdout, []string{"SEVERITY", colType, "TITLE", "DETAIL"}, rows, output.StateCells(0))
}
