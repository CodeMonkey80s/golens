package cli

import (
	"runtime/debug"
	"testing"
)

// TestVersionDefaultCarriesNoReleaseNumber pins the invariant behind injection: the source
// states no release number.
//
// It failed here before. `const Version = "0.1.0"` meant every build in existence reported
// v0.1.0 — including one built after v0.2.0 shipped, which is worse than reporting nothing,
// because a caller checking the version would be told a falsehood.
func TestVersionDefaultCarriesNoReleaseNumber(t *testing.T) {
	if Version != develVersion {
		t.Errorf("Version = %q, want %q\n\nA release number in the source cannot be kept in step with the tag,\nwhich is what this was for. Set it at link time:\n\n  go build -ldflags \"-X github.com/CodeMonkey80s/golens/internal/cli.Version=$(git describe --tags)\"",
			Version, develVersion)
	}
}

// TestReleaseVersionPrefersTheLinkerValue covers the resolution order: a stamped value wins,
// because it is the only one the release process controls.
func TestReleaseVersionPrefersTheLinkerValue(t *testing.T) {
	restore := stubVersion(t, "v1.2.3-from-ldflags")
	defer restore()

	if got := releaseVersion(); got != "v1.2.3-from-ldflags" {
		t.Errorf("releaseVersion() = %q, want the stamped value", got)
	}
}

// TestReleaseVersionFallsBackToBuildMetadata covers the unstamped case, which is what
// `go install module@v0.1.0` produces — the way most people obtain a published module, and
// therefore the path that has to report a real version without anyone passing a flag.
//
// It asserts the fallback invents nothing: the answer is either the sentinel or exactly what
// the toolchain recorded. Go stamps a local build `v0.1.0+dirty` when the tree carries
// uncommitted work, and that is correct to report, so the test must not pin one literal.
func TestReleaseVersionFallsBackToBuildMetadata(t *testing.T) {
	restore := stubVersion(t, develVersion)
	defer restore()

	got := releaseVersion()
	if got == develVersion {
		return // an unstamped build; nothing recorded, nothing invented.
	}

	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" {
		t.Fatalf("releaseVersion() = %q but ReadBuildInfo reported no version", got)
	}
	if got != info.Main.Version {
		t.Errorf("releaseVersion() = %q, want the toolchain's %q", got, info.Main.Version)
	}
}

// stubVersion sets Version for the duration of a test and returns the restore function.
func stubVersion(t *testing.T, v string) func() {
	t.Helper()

	saved := Version
	Version = v
	return func() { Version = saved }
}
