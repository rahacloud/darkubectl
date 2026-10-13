package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"
)

// Growing a disk is the one size change a PersistentVolumeClaim allows, and only
// when the StorageClass sets allowVolumeExpansion. `rawfile-btrfs`, which is what
// both c11 and c13 hand out, does. Shrinking is refused by Kubernetes itself, so
// this command refuses it first rather than letting the app end up with a spec
// its PVC will never satisfy.
//
// Whether a resize takes depends on the app, and BOTH failure modes are silent
// enough to matter. Measured 2026-09-19:
//
//   - A marketplace app (the GitLab runner) accepts the PUT with a 202 and
//     stores nothing. `disk` is not in putDropFields so the new size really is
//     sent, and four reads over the following minute still gave the old one.
//     The field is swallowed the way secret_envs is.
//   - A managed database (mssql-prod) validates it properly and answers
//     400 `حجم دیسک بیشتر از مقدار مجاز است` — the size is above what is
//     allowed. That app sits at 120Gi and even 121 is refused, so the ceiling
//     is not headroom-based; something caps it per app or per cluster.
//
// Hence the read-back after every write: on the first kind of app an API
// success means nothing at all, and errDiskIgnored is the only way a caller
// learns the resize did not happen. On the second kind the API's own error is
// already clear, and this never runs.
//
// That check is the feature, not caution around it. A resize that silently
// no-ops is the worst outcome available for a disk that is filling up, because
// the operator walks away believing it is fixed.
//
// Written 2026-09-19 for the TalaLand GitLab runner, whose 15Gi build cache sat
// at 85-95% full. Its gc sidecar prunes at the 85% threshold, which deletes layer
// directories while BuildKit keeps the cache records that point at them, and
// every build then dies on `failed to prepare sha256:…: no such file or
// directory`. Pruning is the symptom's cure; the disk is the cause's.

const cmdDisk = "disk"

const flagSize = "size"

var (
	errNoDisk = errors.New("this app has no disk, and one cannot be added after creation")

	errDiskSizeRequired = errors.New("--size must be greater than zero")

	errDiskSizeUnreadable = errors.New("the app's disk has no readable size_in_Gi, so a resize cannot be checked")
)

// errDiskIgnored reports the one outcome that looks like success and is not.
//
// Established 2026-09-19 against talaland-untagged-gitlab-runner, where 15 -> 40
// returned success and four reads over the following minute still said 15. Note
// that the OPTIONS schema is no guide here: it reports disk as
// `read_only: false` on an app that then discards it.
func errDiskIgnored(from, want, got int) error {
	return fmt.Errorf(
		"the platform ignored the resize: asked for %dGi, the app still reads %dGi (was %dGi).\n"+
			"The write accepted the field and dropped it, the same way it drops secret_envs, so\n"+
			"there is nothing this tool can do about it. Growing the disk has to go through the\n"+
			"Darkube console, or through Hamravesh support if the console will not offer it either",
		want, got, from)
}

// errDiskShrink explains why the smaller number is refused.
func errDiskShrink(from, to int) error {
	return fmt.Errorf(
		"refusing to shrink the disk from %dGi to %dGi: Kubernetes does not implement "+
			"shrinking a PersistentVolumeClaim, so this would leave the app asking for a size "+
			"its volume will never take", from, to)
}

func newSetDiskCommand() *cli.Command {
	return &cli.Command{
		Name:      cmdDisk,
		Usage:     "Grow an app's persistent disk",
		ArgsUsage: argRefUsage,
		Description: "  darkubectl set disk talaland-untagged-gitlab-runner --size 40\n" +
			"  darkubectl set disk mongodb-stage --size 50 --dry-run\n\n" +
			"Changes disk.size_in_Gi in place, leaving the storage class and the partition\n" +
			"layout as they were read — those two are genuinely fixed at creation.\n\n" +
			"Only growing is possible. Kubernetes does not implement shrinking a PVC, so a\n" +
			"smaller number is refused here rather than accepted into a spec the volume will\n" +
			"never satisfy.\n\n" +
			"The volume is resized by the storage class, not by this call: expansion needs\n" +
			"allowVolumeExpansion on the class (rawfile-btrfs has it), and the filesystem\n" +
			"usually grows online, though some drivers only finish on the next pod start.\n" +
			"Check with `df -h` inside the pod afterwards rather than trusting the spec.",
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:     flagSize,
				Required: true,
				Usage:    "desired size in GiB, which must be larger than the current one",
			},
			&cli.BoolFlag{Name: flagDryRun, Usage: usageDryRun},
			&cli.BoolFlag{Name: flagYes, Aliases: []string{aliasYes}, Usage: usageSkipConfirm},
		},
		Action: setDiskAction,
	}
}

func setDiskAction(ctx context.Context, cmd *cli.Command) error {
	ref := cmd.Args().First()
	if ref == "" {
		return errMissingAppRef
	}
	size := cmd.Int(flagSize)
	if size <= 0 {
		return errDiskSizeRequired
	}

	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	app, err := c.ResolveApp(ctx, ref)
	if err != nil {
		return err
	}

	from, err := currentDiskSize(ctx, c.GetApp, app.ID)
	if err != nil {
		return err
	}
	// applyDiskSize refuses this too, but only once the change is prepared —
	// refuse before showing a diff or asking to confirm "growing" to a smaller
	// number.
	if size < from {
		return errDiskShrink(from, size)
	}

	wrote, err := runAppChange(ctx, cmd, c, app, appChange{
		what:  "grow the disk of",
		apply: func(raw map[string]any) error { return applyDiskSize(raw, size) },
		// The write serializer has been seen accepting `disk` and dropping it,
		// so a 202 here means nothing. Read the size back and believe that.
		took: func(raw map[string]any) bool {
			got, err := diskSizeOf(raw)
			return err == nil && got == size
		},
		notes: []string{"this is one-way. The volume can be grown again later but never shrunk, " +
			"and the app is billed for the new size from now on."},
	})
	if errors.Is(err, errChangeIgnored) {
		got, _ := currentDiskSize(ctx, c.GetApp, app.ID)
		return errDiskIgnored(from, size, got)
	}
	if err != nil || !wrote {
		return err
	}

	fmt.Fprintf(os.Stdout, "app/%s disk set to %dGi\n", app.Name, size)
	fmt.Fprintf(os.Stderr,
		"note: the spec now says %dGi. Whether the filesystem has actually grown is a\n"+
			"      separate question — confirm with `df -h` in the pod, and restart it if the\n"+
			"      driver only finishes the resize on mount.\n", size)
	return nil
}

// applyDiskSize sets disk.size_in_Gi on a normalized app object.
//
// The disk block is rewritten in place so that partitions, storage class and
// set_fsgroup survive untouched: they are creation-only on the write serializer,
// and replacing the object wholesale would drop them.
func applyDiskSize(raw map[string]any, size int) error {
	disk, ok := raw[keyDisk].(map[string]any)
	if !ok || len(disk) == 0 {
		return errNoDisk
	}
	current, ok := disk[keyDiskSize].(float64)
	if !ok {
		return errDiskSizeUnreadable
	}
	if int(current) > size {
		return errDiskShrink(int(current), size)
	}
	disk[keyDiskSize] = size
	return nil
}

// Keys of the disk block as the API spells them.
const (
	keyDisk     = "disk"
	keyDiskSize = "size_in_Gi"
)

// currentDiskSize reads the size an app's disk has right now, so the caller can
// report the change and refuse a shrink before writing anything.
func currentDiskSize(
	ctx context.Context, get func(context.Context, string) (map[string]any, error), id string,
) (int, error) {
	raw, err := get(ctx, id)
	if err != nil {
		return 0, err
	}
	return diskSizeOf(raw)
}

// diskSizeOf reads disk.size_in_Gi out of a raw app object.
func diskSizeOf(raw map[string]any) (int, error) {
	disk, ok := raw[keyDisk].(map[string]any)
	if !ok || len(disk) == 0 {
		return 0, errNoDisk
	}
	// Not jsonInt: its 0 for anything but a number would read as "no disk yet",
	// let a shrink past the check above and turn the read-back into a false
	// "the platform ignored the resize".
	size, ok := disk[keyDiskSize].(float64)
	if !ok {
		return 0, errDiskSizeUnreadable
	}
	return int(size), nil
}
