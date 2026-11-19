package cmd

import (
	"runtime/debug"
	"strings"
)

// devVersion is what a build with no version information reports.
const devVersion = "dev"

// version is set at build time via -ldflags "-X ...cmd.version=...". Release
// builds and the AUR packages set it; `go install` and `go build` do not.
var version = ""

// buildVersion returns the version this binary reports.
func buildVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return resolveVersion(version, nil)
	}

	return resolveVersion(version, info)
}

// resolveVersion picks the version: the linker-provided one first, then the
// module version Go embeds in the binary, then devVersion.
//
// `go install module@version` embeds the resolved tag (v0.15.0). A build from
// a git checkout embeds the tag or a pseudo-version of the commit, with
// "+dirty" when the tree has changes. `go run`, `go test` and a build outside
// version control embed "(devel)" or nothing.
func resolveVersion(linked string, info *debug.BuildInfo) string {
	if linked != "" {
		return linked
	}

	if info == nil {
		return devVersion
	}

	module := info.Main.Version
	if module == "" || module == "(devel)" {
		return devVersion
	}

	// Releases report the version without the leading "v"; do the same.
	return strings.TrimPrefix(module, "v")
}
