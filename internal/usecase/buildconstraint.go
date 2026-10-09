package usecase

import (
	"go/build"
	"go/build/constraint"
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
var releaseTags = func() map[string]bool {
	tags := make(map[string]bool, len(build.Default.ReleaseTags))
	for _, tag := range build.Default.ReleaseTags {
		tags[tag] = true
	}
	return tags
}()

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

// parseBuildConstraint extracts a file's build constraint, returning nil when the file is
// always built.
//
// Both syntaxes are handled by the standard library: constraint.Parse accepts a whole comment
// line, prefix included, and understands //go:build as well as the legacy // +build form. A
// repository may carry either or both, so //go:build wins when present, matching the toolchain.
//
// Legacy lines are combined with AND because that is how the toolchain reads them: each line
// is an OR-list of tags, and consecutive lines all have to hold. "linux darwin" followed by
// "!windows" means (linux || darwin) && !windows.
//
// Lines are passed through untrimmed. constraint.Parse rejects a line with leading whitespace,
// which is how the toolchain reads it too, so trimming here would accept files the compiler
// rejects.
func parseBuildConstraint(src []byte) constraint.Expr {
	var goBuild, plusBuild constraint.Expr

	for _, line := range strings.Split(string(src), "\n") {
		// Constraints live in the header comment block; the package clause ends the search.
		if strings.HasPrefix(line, "package ") {
			break
		}
		switch {
		case constraint.IsGoBuild(line):
			if expr, err := constraint.Parse(line); err == nil {
				goBuild = expr
			}
		case constraint.IsPlusBuild(line):
			if expr, err := constraint.Parse(line); err == nil {
				if plusBuild == nil {
					plusBuild = expr
				} else {
					plusBuild = &constraint.AndExpr{X: plusBuild, Y: expr}
				}
			}
		}
	}

	if goBuild != nil {
		return goBuild
	}
	return plusBuild
}
