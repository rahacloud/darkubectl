package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/rahacloud/darkubectl/internal/output"
	"github.com/urfave/cli/v3"
	"golang.org/x/term"
)

const (
	flagOTP        = "otp"
	flagStorage    = "storage"
	flagKeyAction  = "action"
	flagKeyBucket  = "bucket"
	envOTP         = "DARKUBE_OTP"
	argBucketUsage = "BUCKET"
	argKeyUsage    = "KEY"

	subCreate = "create"
	subDelete = "delete"
	subKey    = "key"
)

var (
	errMissingBucketName = errors.New("a BUCKET name is required")
	errMissingStorageKey = errors.New("a KEY name is required")
	errNoSuchBucket      = errors.New("no bucket with that name in this tenant")
	errNoSuchStorage     = errors.New("no storage with that name or id")
	errNoSuchStorageKey  = errors.New("no storage key with that name in this tenant")
	errPickStorage       = errors.New("the tenant can use several storages; pick one with --storage")
	errNoStorage         = errors.New("no object storage in this tenant")
	errBadKeyAction      = errors.New("unknown key action")
	errKeyNeedsActions   = errors.New("give the key at least one --action: Read, Write or List")
	errKeyNeedsBuckets   = errors.New("give the key at least one --bucket")
	errNoOTP             = errors.New("this change needs your two-factor code: pass --otp, or set " + envOTP)
)

// storageKeyActions are the grants a key can hold, in the order they are shown.
var storageKeyActions = []string{client.StorageRead, client.StorageWrite, client.StorageList}

// newStorageCommand is the write side of object storage. The reads stay under
// `get storage`; `storage list` is the same listing, here so the group stands
// on its own.
func newStorageCommand() *cli.Command {
	yesFlag := &cli.BoolFlag{Name: flagYes, Aliases: []string{aliasYes}, Usage: usageSkipConfirm}
	otpFlag := &cli.StringFlag{
		Name:    flagOTP,
		Usage:   "two-factor (TOTP) code of the signed-in account; asked for on a terminal when omitted",
		Sources: cli.EnvVars(envOTP),
	}
	return &cli.Command{
		Name:  "storage",
		Usage: "Create and remove object-storage buckets and access keys",
		Description: "Buckets live on a storage (`storage list` names them) and are reached over S3 at\n" +
			"the storage's ENDPOINT with an access key. A key is limited to some actions on\n" +
			"some buckets.\n\n" +
			"The platform asks for the account's two-factor code before it touches a key or\n" +
			"changes who can read a bucket. Give it with --otp or " + envOTP + "; on a terminal it\n" +
			"is asked for. It is the code of the Console login, so these commands need that\n" +
			"login rather than an API key.",
		Commands: []*cli.Command{
			{
				Name:    "list",
				Aliases: []string{"ls"},
				Usage:   "List storages with their S3 endpoint, buckets and keys",
				Action:  getStorageAction,
			},
			{
				Name:  "bucket",
				Usage: "Create, delete, publish or hide a bucket",
				Commands: []*cli.Command{
					{
						Name:      subCreate,
						Usage:     "Create a private bucket",
						ArgsUsage: argBucketUsage,
						Description: "  darkubectl storage bucket create acme-media --storage \"Tehran - Asiatech\"\n\n" +
							"Bucket names are shared by every tenant of the storage service, so a plain\n" +
							"name is likely taken; prefix it. The bucket starts private.",
						Flags: []cli.Flag{
							&cli.StringFlag{Name: flagStorage, Usage: "storage to create it on, by name or id (needed when there are several)"},
						},
						Action: bucketCreateAction,
					},
					{
						Name:      subDelete,
						Aliases:   []string{"rm"},
						Usage:     "Delete a bucket and every object in it",
						ArgsUsage: argBucketUsage,
						Flags:     []cli.Flag{otpFlag, yesFlag},
						Action:    bucketDeleteAction,
					},
					{
						Name:      "public",
						Usage:     "Let anyone read the bucket's objects without a key",
						ArgsUsage: argBucketUsage,
						Flags:     []cli.Flag{otpFlag, yesFlag},
						Action:    func(ctx context.Context, cmd *cli.Command) error { return bucketVisibilityAction(ctx, cmd, true) },
					},
					{
						Name:      "private",
						Usage:     "Require a key to read the bucket again",
						ArgsUsage: argBucketUsage,
						Flags:     []cli.Flag{otpFlag, yesFlag},
						Action:    func(ctx context.Context, cmd *cli.Command) error { return bucketVisibilityAction(ctx, cmd, false) },
					},
				},
			},
			{
				Name:  subKey,
				Usage: "Create, widen, rotate or revoke an access key",
				Commands: []*cli.Command{
					{
						Name:      subCreate,
						Usage:     "Create an access key and print it (prints a secret)",
						ArgsUsage: argKeyUsage,
						Description: "  darkubectl storage key create media-rw --bucket acme-media --action Read,Write,List\n\n" +
							"The pair is printed once, on stdout, as the AWS_* variables an S3 client reads,\n" +
							"with the endpoint; -o json prints it as an object instead. The secret cannot be\n" +
							"read again: `storage key rotate` issues a new pair when it is lost.",
						Flags:  []cli.Flag{keyActionFlag(), keyBucketFlag(), otpFlag},
						Action: keyCreateAction,
					},
					{
						Name:      "grant",
						Aliases:   []string{"update"},
						Usage:     "Add actions or buckets to a key",
						ArgsUsage: argKeyUsage,
						Description: "  darkubectl storage key grant media-ro --action Write\n" +
							"  darkubectl storage key grant media-rw --bucket acme-backups\n\n" +
							"What is given is added to what the key has, and the pair does not change. The\n" +
							"platform has no way to take a grant back: to narrow a key, delete it and create\n" +
							"it again, which also issues a new pair.",
						Flags:  []cli.Flag{keyActionFlag(), keyBucketFlag(), otpFlag},
						Action: keyGrantAction,
					},
					{
						Name:      "rotate",
						Usage:     "Issue a new pair for a key and print it (prints a secret)",
						ArgsUsage: argKeyUsage,
						Flags:     []cli.Flag{otpFlag, yesFlag},
						Action:    keyRotateAction,
					},
					{
						Name:      subDelete,
						Aliases:   []string{"rm"},
						Usage:     "Revoke an access key",
						ArgsUsage: argKeyUsage,
						Flags:     []cli.Flag{otpFlag, yesFlag},
						Action:    keyDeleteAction,
					},
				},
			},
		},
	}
}

func keyActionFlag() cli.Flag {
	return &cli.StringSliceFlag{Name: flagKeyAction, Usage: "what the key may do: Read, Write, List (repeat or separate with commas)"}
}

func keyBucketFlag() cli.Flag {
	return &cli.StringSliceFlag{Name: flagKeyBucket, Usage: "bucket the key covers, by name (repeat or separate with commas)"}
}

// resolveOTP returns the two-factor code for a write that needs one: the flag
// or its environment variable, or an answer typed on a terminal. A code is
// never stored: it is good for thirty seconds.
func resolveOTP(cmd *cli.Command) (string, error) {
	if code := strings.TrimSpace(cmd.String(flagOTP)); code != "" {
		return code, nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", errNoOTP
	}
	fmt.Fprint(os.Stderr, "Two-factor code: ")
	line, ok := readLine()
	if code := strings.TrimSpace(line); ok && code != "" {
		return code, nil
	}
	return "", errNoOTP
}

// findBucket locates a bucket by name across the tenant's storages.
func findBucket(storages []client.Storage, name string) (*client.Storage, *client.Bucket, error) {
	for i := range storages {
		for j := range storages[i].Buckets {
			if storages[i].Buckets[j].Name == name {
				return &storages[i], &storages[i].Buckets[j], nil
			}
		}
	}
	return nil, nil, fmt.Errorf("%w: %q", errNoSuchBucket, name)
}

// findStorageKey locates a key by name across the tenant's storages.
func findStorageKey(storages []client.Storage, name string) (*client.Storage, *client.StorageKey, error) {
	for i := range storages {
		for j := range storages[i].Keys {
			if storages[i].Keys[j].Name == name {
				return &storages[i], &storages[i].Keys[j], nil
			}
		}
	}
	return nil, nil, fmt.Errorf("%w: %q", errNoSuchStorageKey, name)
}

// pickStorage resolves --storage, or the tenant's only storage without it.
func pickStorage(storages []client.Storage, ref string) (*client.Storage, error) {
	names := make([]string, 0, len(storages))
	for i := range storages {
		if ref != "" && (storages[i].ID == ref || strings.EqualFold(storages[i].Name, ref)) {
			return &storages[i], nil
		}
		names = append(names, fmt.Sprintf("%q", storages[i].Name))
	}
	switch {
	case len(storages) == 0:
		return nil, errNoStorage
	case ref != "":
		return nil, fmt.Errorf("%w: %q (there is %s)", errNoSuchStorage, ref, strings.Join(names, ", "))
	case len(storages) > 1:
		return nil, fmt.Errorf("%w: %s", errPickStorage, strings.Join(names, ", "))
	}
	return &storages[0], nil
}

// parseKeyActions canonicalises --action values, which may be comma-separated
// and in any letter case, into the API's spelling and display order.
func parseKeyActions(values []string) ([]string, error) {
	var out []string
	for _, value := range splitCommas(values) {
		i := slices.IndexFunc(storageKeyActions, func(a string) bool { return strings.EqualFold(a, value) })
		if i < 0 {
			return nil, fmt.Errorf("%w %q: choose from %s", errBadKeyAction, value, strings.Join(storageKeyActions, ", "))
		}
		if !slices.Contains(out, storageKeyActions[i]) {
			out = append(out, storageKeyActions[i])
		}
	}
	slices.SortFunc(out, func(a, b string) int {
		return slices.Index(storageKeyActions, a) - slices.Index(storageKeyActions, b)
	})
	return out, nil
}

// splitCommas flattens repeated, comma-separated flag values, dropping blanks.
func splitCommas(values []string) []string {
	var out []string
	for _, value := range values {
		for part := range strings.SplitSeq(value, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

// bucketIDs maps bucket names to ids, all of which must sit on one storage: a
// key belongs to a storage and cannot span two.
func bucketIDs(storages []client.Storage, names []string) (*client.Storage, []string, error) {
	var home *client.Storage
	ids := make([]string, 0, len(names))
	for _, name := range names {
		storage, bucket, err := findBucket(storages, name)
		if err != nil {
			return nil, nil, err
		}
		if home != nil && home.ID != storage.ID {
			return nil, nil, fmt.Errorf("%w: %q is on %q, the others on %q", errBucketsSpanStorages, name, storage.Name, home.Name)
		}
		home = storage
		ids = append(ids, bucket.ID)
	}
	return home, ids, nil
}

var errBucketsSpanStorages = errors.New("a key covers buckets of one storage")

func bucketCreateAction(ctx context.Context, cmd *cli.Command) error {
	name := cmd.Args().First()
	if name == "" {
		return errMissingBucketName
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	storages, err := c.Storages(ctx)
	if err != nil {
		return err
	}
	storage, err := pickStorage(storages, cmd.String(flagStorage))
	if err != nil {
		return err
	}
	if _, err := c.CreateBucket(ctx, storage.ID, name); err != nil {
		if errors.Is(err, client.ErrBucketNameTaken) {
			return fmt.Errorf("%w; names are shared across all tenants, so try a prefixed one", err)
		}
		return err
	}
	// The bucket is "creating" for a few seconds; wait for it, since a key or an
	// upload aimed at it before then fails.
	state := "creating"
	_, _ = retryReadBack(ctx, func(ctx context.Context) (bool, error) {
		fresh, err := c.Storages(ctx)
		if err != nil {
			return false, err
		}
		if _, bucket, err := findBucket(fresh, name); err == nil {
			state = bucket.Status
		}
		return state == "active", nil
	})
	fmt.Fprintf(os.Stdout, "bucket/%s created on %q (%s)\n", name, storage.Name, state)
	return nil
}

func bucketDeleteAction(ctx context.Context, cmd *cli.Command) error {
	name := cmd.Args().First()
	if name == "" {
		return errMissingBucketName
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	storages, err := c.Storages(ctx)
	if err != nil {
		return err
	}
	storage, bucket, err := findBucket(storages, name)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "About to delete bucket %q (%s) on %q in tenant %q, with every object in it. This cannot be undone.\n",
		bucket.Name, bucketSize(bucket.Size), storage.Name, c.Org)
	if !cmd.Bool(flagYes) && !confirmExact("Type the bucket name to confirm: ", bucket.Name) {
		return errAborted
	}
	otp, err := resolveOTP(cmd)
	if err != nil {
		return err
	}
	if err := c.DeleteBucket(ctx, bucket.ID, otp); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "bucket/%s deleted\n", bucket.Name)
	return nil
}

func bucketVisibilityAction(ctx context.Context, cmd *cli.Command, public bool) error {
	name := cmd.Args().First()
	if name == "" {
		return errMissingBucketName
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	storages, err := c.Storages(ctx)
	if err != nil {
		return err
	}
	_, bucket, err := findBucket(storages, name)
	if err != nil {
		return err
	}
	word := map[bool]string{true: "public", false: "private"}[public]
	if bucket.IsPublic == public {
		fmt.Fprintf(os.Stdout, "bucket/%s unchanged: it is already %s\n", bucket.Name, word)
		return nil
	}
	if public {
		fmt.Fprintf(os.Stderr, "About to make bucket %q in tenant %q public: anyone with the URL can read its objects.\n", bucket.Name, c.Org)
		if !cmd.Bool(flagYes) && !confirm() {
			return errAborted
		}
	}
	otp, err := resolveOTP(cmd)
	if err != nil {
		return err
	}
	if err := c.SetBucketPublic(ctx, bucket.ID, public, otp); err != nil {
		return err
	}
	stored, err := retryReadBack(ctx, func(ctx context.Context) (bool, error) {
		fresh, err := c.Storages(ctx)
		if err != nil {
			return false, err
		}
		_, now, err := findBucket(fresh, name)
		return err == nil && now.IsPublic == public, nil
	})
	if err != nil {
		return err
	}
	if !stored {
		return fmt.Errorf("%w: bucket %q still reads back as not %s", errChangeIgnored, bucket.Name, word)
	}
	fmt.Fprintf(os.Stdout, "bucket/%s is now %s\n", bucket.Name, word)
	return nil
}

func keyCreateAction(ctx context.Context, cmd *cli.Command) error {
	name := cmd.Args().First()
	if name == "" {
		return errMissingStorageKey
	}
	format, err := outputFormat(cmd)
	if err != nil {
		return err
	}
	actions, err := parseKeyActions(cmd.StringSlice(flagKeyAction))
	if err != nil {
		return err
	}
	if len(actions) == 0 {
		return errKeyNeedsActions
	}
	buckets := splitCommas(cmd.StringSlice(flagKeyBucket))
	if len(buckets) == 0 {
		return errKeyNeedsBuckets
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	storages, err := c.Storages(ctx)
	if err != nil {
		return err
	}
	storage, ids, err := bucketIDs(storages, buckets)
	if err != nil {
		return err
	}
	otp, err := resolveOTP(cmd)
	if err != nil {
		return err
	}
	key, err := c.CreateStorageKey(ctx, name, actions, ids, otp)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Storage key %q created: %s on %s. This is the only time the secret is shown:\n",
		name, strings.Join(key.Actions, ", "), strings.Join(key.Buckets, ", "))
	return printStorageKeySecret(format, key, storage.Endpoint())
}

func keyGrantAction(ctx context.Context, cmd *cli.Command) error {
	name := cmd.Args().First()
	if name == "" {
		return errMissingStorageKey
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	storages, err := c.Storages(ctx)
	if err != nil {
		return err
	}
	_, key, err := findStorageKey(storages, name)
	if err != nil {
		return err
	}
	// The route takes the full lists and refuses any that drop a grant, so what
	// is asked for is added to what the key holds.
	more, err := parseKeyActions(cmd.StringSlice(flagKeyAction))
	if err != nil {
		return err
	}
	actions, err := parseKeyActions(append(slices.Clone(key.Actions), more...))
	if err != nil {
		return err
	}
	buckets := slices.Clone(key.Buckets)
	for _, bucket := range splitCommas(cmd.StringSlice(flagKeyBucket)) {
		if !slices.Contains(buckets, bucket) {
			buckets = append(buckets, bucket)
		}
	}
	if sameSet(actions, key.Actions) && sameSet(buckets, key.Buckets) {
		fmt.Fprintf(os.Stdout, "storagekey/%s unchanged: it already has %s on %s\n",
			name, strings.Join(actions, ", "), strings.Join(buckets, ", "))
		return nil
	}
	_, ids, err := bucketIDs(storages, buckets)
	if err != nil {
		return err
	}
	otp, err := resolveOTP(cmd)
	if err != nil {
		return err
	}
	if err := c.UpdateStorageKey(ctx, name, actions, ids, otp); err != nil {
		return err
	}
	stored, err := retryReadBack(ctx, func(ctx context.Context) (bool, error) {
		fresh, err := c.Storages(ctx)
		if err != nil {
			return false, err
		}
		_, now, err := findStorageKey(fresh, name)
		return err == nil && sameSet(now.Actions, actions) && sameSet(now.Buckets, buckets), nil
	})
	if err != nil {
		return err
	}
	if !stored {
		return fmt.Errorf("%w: key %q does not read back with the new grants", errChangeIgnored, name)
	}
	fmt.Fprintf(os.Stdout, "storagekey/%s now has %s on %s\n", name, strings.Join(actions, ", "), strings.Join(buckets, ", "))
	return nil
}

func keyRotateAction(ctx context.Context, cmd *cli.Command) error {
	name := cmd.Args().First()
	if name == "" {
		return errMissingStorageKey
	}
	format, err := outputFormat(cmd)
	if err != nil {
		return err
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	storages, err := c.Storages(ctx)
	if err != nil {
		return err
	}
	storage, key, err := findStorageKey(storages, name)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "About to rotate storage key %q in tenant %q. Whatever uses the current pair stops working.\n", key.Name, c.Org)
	if !cmd.Bool(flagYes) && !confirm() {
		return errAborted
	}
	otp, err := resolveOTP(cmd)
	if err != nil {
		return err
	}
	fresh, err := c.RotateStorageKey(ctx, key.Name, otp)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Storage key %q rotated. This is the only time the new secret is shown:\n", key.Name)
	return printStorageKeySecret(format, fresh, storage.Endpoint())
}

func keyDeleteAction(ctx context.Context, cmd *cli.Command) error {
	name := cmd.Args().First()
	if name == "" {
		return errMissingStorageKey
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	storages, err := c.Storages(ctx)
	if err != nil {
		return err
	}
	_, key, err := findStorageKey(storages, name)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "About to revoke storage key %q (%s on %s) in tenant %q. Anything using it stops working.\n",
		key.Name, strings.Join(key.Actions, ", "), strings.Join(key.Buckets, ", "), c.Org)
	if !cmd.Bool(flagYes) && !confirm() {
		return errAborted
	}
	otp, err := resolveOTP(cmd)
	if err != nil {
		return err
	}
	if err := c.DeleteStorageKey(ctx, key.Name, otp); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "storagekey/%s revoked\n", key.Name)
	return nil
}

// printStorageKeySecret prints a freshly minted pair on stdout: as an object
// for -o json|yaml, otherwise as the variables an S3 client reads, ready for an
// env file or `set env --secret --env-file`.
func printStorageKeySecret(format output.Format, key *client.StorageKeySecret, endpoint string) error {
	if handled, err := output.Structured(os.Stdout, format, key); handled {
		return err
	}
	fmt.Fprintf(os.Stdout, "AWS_ACCESS_KEY_ID=%s\nAWS_SECRET_ACCESS_KEY=%s\n", key.AccessKey, key.SecretKey)
	if endpoint != "" {
		fmt.Fprintf(os.Stdout, "AWS_ENDPOINT_URL=%s\n", endpoint)
	}
	return nil
}

// sameSet reports whether two string lists hold the same members.
func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, v := range a {
		if !slices.Contains(b, v) {
			return false
		}
	}
	return true
}
