package cmd

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/rahacloud/darkubectl/internal/output"
	"github.com/urfave/cli/v3"
)

// newGetStorageCommand lists the tenant's object storages. The reason this
// exists is the ENDPOINT column: an S3 client needs that address, it is a
// per-tenant name under hs3.ir with a random suffix so it cannot be derived
// from the tenant slug, and it is served by /storage/v2/domain/ rather than by
// the storage or bucket route anyone would look at first.
func newGetStorageCommand() *cli.Command {
	return &cli.Command{
		Name:    "storage",
		Aliases: []string{"storages", "buckets", "bucket", "s3"},
		Usage:   "List object storages, their S3 endpoint, buckets and keys",
		Description: "The ENDPOINT column is what an S3 client wants as its endpoint URL,\n" +
			"for example AWS_S3_ENDPOINT_URL for django-storages or --endpoint-url\n" +
			"for the aws CLI. Region is `us-east-1` whatever the location, because\n" +
			"the backend is SeaweedFS rather than Amazon.\n\n" +
			"Secret keys are not shown and cannot be: the platform returns a secret\n" +
			"once, from the call that creates the key, and never again.",
		Action: getStorageAction,
	}
}

func getStorageAction(ctx context.Context, cmd *cli.Command) error {
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	format, err := outputFormat(cmd)
	if err != nil {
		return err
	}
	storages, err := c.Storages(ctx)
	if err != nil {
		return err
	}
	if handled, err := output.Structured(os.Stdout, format, storages); handled {
		return err
	}
	if len(storages) == 0 {
		fmt.Fprintln(os.Stderr, "no object storage in this tenant")
		return nil
	}

	rows := make([][]string, 0, len(storages))
	for _, s := range storages {
		rows = append(rows, []string{
			dash(s.Name),
			dash(s.StorageClass),
			dash(s.Endpoint()),
			strconv.Itoa(len(s.Buckets)),
			strconv.Itoa(len(s.Keys)),
		})
	}
	if err := output.StyledTable(os.Stdout,
		[]string{colName, "CLASS", "ENDPOINT", "BUCKETS", "KEYS"}, rows, nil); err != nil {
		return err
	}

	for _, s := range storages {
		if err := printStorageDetail(s); err != nil {
			return err
		}
	}
	return nil
}

// printStorageDetail prints the buckets and keys of one storage under its own
// heading, because the table above can only count them.
func printStorageDetail(s client.Storage) error {
	if len(s.Buckets) > 0 {
		if err := output.PrintSectionHeader(os.Stdout, "buckets on "+s.Name); err != nil {
			return err
		}
		rows := make([][]string, 0, len(s.Buckets))
		for _, b := range s.Buckets {
			rows = append(rows, []string{
				dash(b.Name), dash(b.Status), bucketSize(b.Size), yesNo(b.IsPublic),
			})
		}
		if err := output.StyledTable(os.Stdout,
			[]string{colName, colState, "SIZE", "PUBLIC"}, rows, nil); err != nil {
			return err
		}
	}
	if len(s.Keys) > 0 {
		if err := output.PrintSectionHeader(os.Stdout,
			"keys on "+s.Name+" (a secret is returned only when the key is created)"); err != nil {
			return err
		}
		rows := make([][]string, 0, len(s.Keys))
		for _, k := range s.Keys {
			rows = append(rows, []string{
				dash(k.Name), dash(strings.Join(k.Actions, ", ")), dash(strings.Join(k.Buckets, ", ")),
			})
		}
		if err := output.StyledTable(os.Stdout,
			[]string{colName, "ACTIONS", "BUCKETS"}, rows, nil); err != nil {
			return err
		}
	}
	return nil
}

// bucketSize renders the byte count, which this API returns as a string.
func bucketSize(raw string) string {
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return dash(raw)
	}
	return humanBytes(n)
}
