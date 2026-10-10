package usecase

import "testing"

// TestImplicitFilenameConstraints covers the constraint Go infers from a file's name, which
// applies independently of any //go:build line.
//
// It matters because syscall/env_windows.go carries no build tag at all -- it is Windows-only
// purely by name -- and reading only the comment treated it as built everywhere. On the standard
// library that produced 70,854 false redeclarations, which is the failure mode that gets a checker
// switched off entirely.
func TestImplicitFilenameConstraints(t *testing.T) {
	cases := map[string]string{
		"a_windows.go":          "windows",
		"a_linux.go":            "linux",
		"handler_linux_test.go": "linux",
		"x_amd64.go":            "amd64",
		"y_linux_amd64.go":      "linux && amd64",
		"zero_copy_linux.go":    "linux",
		"os_only_solaris.go":    "solaris",
		"syscall/env_unix.go":   "", // "unix" is not a GOOS
		"net/http/h2_bundle.go": "", // "bundle" is neither
		"z.go":                  "",
		"app.go":                "",
		"_ignored.go":           "", // ignored by the go tool
		".hidden.go":            "",
		"notes_linux.md-ish":    "",
		"a_windows_test.go":     "windows",
		"a_darwin_arm64.go":     "darwin && arm64",
		"weird_mips64x.go":      "", // "mips64x" is not a GOARCH
		"weird_mips64le.go":     "mips64le",
		// Only the components after the first underscore are candidates, and only the last
		// one or two. A middle word that merely looks like a platform is not one.
		"prefix_with_underscore.go": "",
		"prefix_windows_middle.go":  "",
	}

	for path, want := range cases {
		if got := implicitConstraint(path); got != want {
			t.Errorf("implicitConstraint(%q) = %q, want %q", path, got, want)
		}
	}
}

// TestFilenameAndTagConstraintsCombine covers the case where a name suffix and a build tag both
// apply. Go intersects them, so a file may be narrower than either alone.
func TestFilenameAndTagConstraintsCombine(t *testing.T) {
	// linux by name, cgo by tag: built only where both hold.
	v := constraintVector(fileConstraint("foo_linux.go", "cgo", false))
	var cgoLinux, linuxNoCgo bool
	for i, cfg := range buildConfigs {
		if !v[i] {
			continue
		}
		if cfg.cgo && cfg.goos == "linux" {
			cgoLinux = true
		}
		if !cfg.cgo && cfg.goos == "linux" {
			linuxNoCgo = true
		}
	}
	if !cgoLinux {
		t.Error("linux+cgo configuration should include the file")
	}
	if linuxNoCgo {
		t.Error("linux without cgo should not include the file")
	}
}

// TestCgoFilesAreExcludedWhenCgoIsOff covers the third constraint, which is not in the file's
// name or its tags at all: a file importing "C" is dropped when cgo is disabled.
//
// crypto/internal/sysrand/internal/seccomp holds such a file with no tag beside one guarded by
// "//go:build !linux || !cgo". Without the cgo term both are built on a cgo-less linux, and the
// package does not compile, which it does.
func TestCgoFilesAreExcludedWhenCgoIsOff(t *testing.T) {
	v := constraintVector(fileConstraint("seccomp_linux.go", "", true))
	for i, cfg := range buildConfigs {
		if v[i] && !cfg.cgo {
			t.Errorf("a cgo file is included in a cgo-less configuration (%s/%s)", cfg.goos, cfg.goarch)
		}
	}

	// And it must still conflict with an untagged file when cgo is on, or the term is useless.
	if !couldCoBuild(
		constraintVector(fileConstraint("a.go", "", true)),
		constraintVector(fileConstraint("a.go", "", false)),
	) {
		t.Error("a cgo file should still co-build with an ordinary file when cgo is enabled")
	}
}

// TestPlatformVariantsDoNotConflict is the behaviour the whole matrix exists to produce: two
// files that no build ever sees together are silent.
func TestPlatformVariantsDoNotConflict(t *testing.T) {
	pairs := [][2]string{
		{"a_linux.go", "a_windows.go"},
		{"a_linux.go", "a_darwin.go"},
		{"a_linux_amd64.go", "a_linux_arm64.go"},
		// linux/386 and linux/sparc do not look like complementary platforms but only the
		// second is real. An invented pair made "mips || mipsle" and "freebsd" both hold.
		{"a_mipsle.go", "a_freebsd.go"},
		{"a_linux.go", "a_sparc.go"},
	}
	for _, p := range pairs {
		if couldCoBuild(
			constraintVector(fileConstraint(p[0], "", false)),
			constraintVector(fileConstraint(p[1], "", false)),
		) {
			t.Errorf("%s and %s cannot be compiled together, but were reported as conflicting", p[0], p[1])
		}
	}
}

// TestCoBuildableFilesStillConflict is the positive control. Without it a rule that suppressed
// everything would pass every test above.
func TestCoBuildableFilesStillConflict(t *testing.T) {
	if !couldCoBuild(
		constraintVector(fileConstraint("one.go", "", false)),
		constraintVector(fileConstraint("two.go", "", false)),
	) {
		t.Error("two untagged files must conflict")
	}
	if !couldCoBuild(
		constraintVector(fileConstraint("a_linux.go", "", false)),
		constraintVector(fileConstraint("one.go", "", false)),
	) {
		t.Error("a_linux.go and an untagged file must conflict: both are built on linux")
	}
	// The arch half of a filename constrains alone: linux/386 is a real pair.
	if !couldCoBuild(
		constraintVector(fileConstraint("a_386.go", "", false)),
		constraintVector(fileConstraint("a_linux.go", "", false)),
	) {
		t.Error("a_386.go is built on linux/386, which co-builds with a_linux.go")
	}
}

// TestRepeatableNamesAreNotConflicts covers Go's two exceptions to one name per package:
// multiple init functions are explicitly permitted, and the blank identifier is never a name.
func TestRepeatableNamesAreNotConflicts(t *testing.T) {
	for _, name := range []string{"init", "_"} {
		if !isRepeatableName(name) {
			t.Errorf("%q should be repeatable", name)
		}
	}
	for _, name := range []string{"F", "User", "initFn", "initialise"} {
		if isRepeatableName(name) {
			t.Errorf("%q is not repeatable in Go and must not be exempt", name)
		}
	}
}
