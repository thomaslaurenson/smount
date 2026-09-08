package cmd

import (
	"fmt"
	"regexp"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"
)

// devVersion is what an unstamped build reports.
const devVersion = "dev"

// Version is the smount version, injected at build time via ldflags.
//
// The injection is -X github.com/thomaslaurenson/smount/cmd.Version=...
// It falls back to "dev" for local builds that do not set a value.
var Version = devVersion

// init fills in the version for a build that set no ldflags.
//
// "go install github.com/thomaslaurenson/smount@v1.2.3" runs the toolchain
// rather than the Makefile, so nothing stamps Version and a tagged release
// would otherwise report itself as "dev". The toolchain records the module
// version in the binary, so read it back from there.
//
// This cannot be an initialiser on Version itself. The linker writes an -X
// value into the data segment, and a variable with a runtime initialiser has
// that value overwritten the moment the initialiser runs.
func init() {
	if info, ok := debug.ReadBuildInfo(); ok {
		Version = versionFrom(Version, info.Main.Version)
	}
}

// versionFrom picks the version to report, given the one ldflags stamped in and
// the one the toolchain recorded.
//
// An injected version always wins; it is the more specific answer and the only
// one a release build has. The module version is used only to rescue a "dev"
// that came from "go install <module>@<tag>".
//
// The leading "v" is dropped so that a go install build reports the same string
// as a release build, where goreleaser has already stripped it.
func versionFrom(injected, module string) string {
	if injected != devVersion || !isReleaseVersion(module) {
		return injected
	}
	return strings.TrimPrefix(module, "v")
}

// pseudoVersion matches the timestamp and commit tail the toolchain appends
// when a build has no tag to name itself after.
//
// The separator before the timestamp is a "-" only when there was no earlier
// tag ("v0.0.0-<stamp>-<commit>"). Above an existing tag the toolchain builds a
// prerelease of the next patch and the separator is a "." instead
// ("v1.2.4-0.<stamp>-<commit>"), so both have to match. The commit tail is what
// makes this a pseudo-version rather than a tag that merely looks like a date.
var pseudoVersion = regexp.MustCompile(`[-.][0-9]{14}-[0-9a-f]{12}$`)

// isReleaseVersion reports whether module names a published tag.
//
// Only a tag is worth reporting as the version. A build from a working tree is
// recorded as "(devel)", or, once the toolchain has VCS information, as a
// pseudo-version carrying a timestamp and a commit and a "+dirty" suffix. None
// of those is a release, and printing one would make a local build announce
// itself as a published version.
func isReleaseVersion(module string) bool {
	switch {
	case module == "", module == "(devel)":
		return false
	case strings.Contains(module, "+"):
		// Build metadata, which is where the toolchain records a dirty tree.
		return false
	default:
		return !pseudoVersion.MatchString(module)
	}
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the smount version",
		Args:  cobra.NoArgs,
		// The root builds the UI in its PersistentPreRunE, which version has no
		// use for: it prints one line and never prompts or styles anything.
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "smount version %s\n", Version)
		},
	}
}
