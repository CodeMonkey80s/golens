package usecase

import (
	"go/build/constraint"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// This file answers one question: could two files ever be compiled into the same binary?
//
// The duplicate-declaration check needs it. golens parses every .go file in a tree regardless
// of build constraints, which is right for a structural tool -- an agent asking what a
// directory contains wants all of it -- but wrong for a compiler-shaped check. A repository
// with filesystems_linux.go, filesystems_darwin.go and filesystems_windows.go declares
// isFuseFs in all three, no build ever sees two of them, and the Go compiler is silent.
// Reporting those as redeclarations would be pure noise, and 23 such diagnostics on one small
// repository is enough to make an agent distrust every line of check output.
//
// The test is satisfiability: two files conflict only if some configuration includes both.

// buildConfig is one point in the space of builds a file might participate in.
//
// Release tags and custom -tags are not modelled per configuration; they are handled by the
// tag lookup in configSatisfied, which consults the process-wide values.
type buildConfig struct {
	goos   string
	goarch string
	cgo    bool
}

// buildConfigs is the matrix of configurations every constraint is evaluated over.
//
// GOOS and GOARCH are cross-producted because a constraint may name either, or both. cgo is
// varied because it is a build tag as well as an environment variable, and a repository can
// legitimately guard a file with "cgo" and another with "!cgo".
var buildConfigs = allConfigs()

func allConfigs() []buildConfig {
	gooses := []string{
		"aix", "android", "darwin", "dragonfly", "freebsd", "hurd", "illumos", "ios", "js",
		"linux", "netbsd", "openbsd", "plan9", "solaris", "wasip1", "windows",
	}
	goarches := []string{
		"386", "amd64", "arm", "arm64", "loong64", "mips", "mips64", "mips64le", "mipsle",
		"ppc64", "ppc64le", "riscv64", "s390x", "sparc64", "wasm",
	}

	out := make([]buildConfig, 0, len(gooses)*len(goarches)*2)
	for _, goos := range gooses {
		for _, goarch := range goarches {
			for _, cgo := range []bool{false, true} {
				out = append(out, buildConfig{goos: goos, goarch: goarch, cgo: cgo})
			}
		}
	}
	return out
}

// unixOS is the set of GOOS values satisfying the "unix" build tag.
var unixOS = map[string]bool{
	"aix": true, "android": true, "darwin": true, "dragonfly": true, "freebsd": true,
	"hurd": true, "illumos": true, "ios": true, "linux": true, "netbsd": true,
	"openbsd": true, "solaris": true,
}

// releaseTags is the set of go1.N tags satisfied by the running toolchain, used to evaluate a
// constraint such as "//go:build go1.21".
//
// The tags are derived from runtime.Version rather than read from go/build.Default.ReleaseTags.
// go/build imports go/ast, and internal/arch forbids any package outside engine/goast from
// doing that -- the rule that keeps a Tree-Sitter engine additive. go/build/constraint is
// clean, so only the release-tag list is reconstructed here.
var releaseTags = buildReleaseTags(runtime.Version())

// buildReleaseTags returns the go1.N tags implied by a toolchain version string.
//
// "go1.27.2", "go1.27.2-X:nodwarf5 linux/amd64" and "go1.21rc1" all yield go1.1 through the
// minor version. A version it cannot read yields no tags, which is the safe direction: an
// unsatisfied tag suppresses a diagnostic rather than inventing one.
func buildReleaseTags(version string) map[string]bool {
	tags := make(map[string]bool)

	v := strings.TrimPrefix(version, "go")
	// Drop any toolchain suffix: "1.27.2-X:nodwarf5 linux/amd64" -> "1.27.2".
	if i := strings.IndexAny(v, " -+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) < 2 {
		return tags
	}

	// Take the leading digits of the minor segment, so a prerelease such as
	// "go1.21rc1" still yields go1.1 through go1.21.
	minor := leadingDigits(parts[1])
	if minor == 0 {
		return tags
	}
	for i := 1; i <= minor; i++ {
		tags["go1."+strconv.Itoa(i)] = true
	}
	return tags
}

// leadingDigits returns the integer prefix of s, or 0 when s does not start with a digit.
func leadingDigits(s string) int {
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0
	}
	n, err := strconv.Atoi(s[:end])
	if err != nil {
		return 0
	}
	return n
}

// customTags holds the -tags values this process was built with. golens does not accept a
// -tags flag, so this is empty and any custom tag evaluates false. A file guarded by a custom
// tag is therefore treated as never built, which is the safe direction: it suppresses a
// diagnostic rather than inventing one.
var customTags = map[string]bool{}

// compiler is the toolchain tag. golens is built with gc; a constraint naming gccgo is not
// satisfied.
const compiler = "gc"

// configSatisfied reports whether the build tags for one configuration are satisfied.
//
// An unrecognised tag is false. That is the deliberate bias throughout: golens cannot know
// every tag a downstream user might pass, so it assumes the narrowest reading and stays quiet
// rather than claiming a conflict it cannot demonstrate.
func (c buildConfig) satisfied(tag string) bool {
	switch tag {
	case c.goos, c.goarch:
		return true
	case "cgo":
		return c.cgo
	case "unix":
		return unixOS[c.goos]
	case compiler:
		return true
	}
	if releaseTags[tag] || customTags[tag] {
		return true
	}
	return false
}

// alwaysBuilt is the inclusion vector of a file with no build constraint: it is in every
// configuration, so it conflicts with any file that is in any configuration.
var alwaysBuilt = func() []bool {
	v := make([]bool, len(buildConfigs))
	for i := range v {
		v[i] = true
	}
	return v
}()

// constraintVectorCache memoises inclusion vectors by constraint text. A repository has far
// fewer distinct constraints than files -- usually one per platform -- so this collapses the
// work to a handful of evaluations.
var (
	constraintVectorMu    sync.Mutex
	constraintVectorCache = map[string][]bool{}
)

// constraintVector returns the configurations in which a file with this constraint is built.
func constraintVector(expr constraint.Expr) []bool {
	if expr == nil {
		return alwaysBuilt
	}

	key := expr.String()

	constraintVectorMu.Lock()
	defer constraintVectorMu.Unlock()

	if v, ok := constraintVectorCache[key]; ok {
		return v
	}

	v := make([]bool, len(buildConfigs))
	for i, cfg := range buildConfigs {
		v[i] = expr.Eval(cfg.satisfied)
	}
	constraintVectorCache[key] = v
	return v
}

// couldCoBuild reports whether two files' build constraints can be satisfied at the same time,
// which is what makes two declarations of one name an actual conflict.
func couldCoBuild(a, b []bool) bool {
	if len(a) != len(b) {
		// Only reachable if a vector were built against a different matrix.
		return true
	}
	for i := range a {
		if a[i] && b[i] {
			return true
		}
	}
	return false
}

// parseBuildConstraint turns a normalised //go:build expression into an evaluable Expr,
// returning nil when text is empty and the file is therefore always built.
//
// The engine owns the source-level scan and normalises both syntaxes; this only re-parses the
// result so the expression can be evaluated over the configuration matrix.
func parseBuildConstraint(text string) constraint.Expr {
	if text == "" {
		return nil
	}
	expr, err := constraint.Parse("//go:build " + text)
	if err != nil {
		return nil
	}
	return expr
}
