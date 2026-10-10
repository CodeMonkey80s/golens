package usecase

import (
	"go/build/constraint"
	"path/filepath"
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
	out := make([]buildConfig, 0, len(knownPairs)*2)
	for _, pair := range knownPairs {
		goos, goarch, _ := strings.Cut(pair, "/")
		for _, cgo := range []bool{false, true} {
			out = append(out, buildConfig{goos: goos, goarch: goarch, cgo: cgo})
		}
	}
	return out
}

// knownPairs are the GOOS/GOARCH combinations the toolchain can actually build, taken from
// `go tool dist list` -- 47 of them, against 210 in a full cross-product.
//
// This matters more than it looks. A constraint is only ever as true as the configurations it is
// evaluated over, and an invented pair makes real constraints look satisfied. Under
// freebsd/mipsle, a fictional combination, "//go:build mips || mipsle" held while
// "//go:build freebsd" also held, so sysnum_linux_mipsx.go appeared to co-build with
// sysnum_freebsd.go.
//
// This is a snapshot rather than a query. golens is stateless and answers in microseconds, so
// shelling out to `go tool dist list` per check is not available, and the tool deliberately has no
// module or toolchain resolution. A stale entry means a newly added platform is not modelled,
// which suppresses a diagnostic rather than inventing one -- the safe direction.
var knownPairs = []string{
	"aix/ppc64",
	"android/386",
	"android/amd64",
	"android/arm",
	"android/arm64",
	"darwin/amd64",
	"darwin/arm64",
	"dragonfly/amd64",
	"freebsd/386",
	"freebsd/amd64",
	"freebsd/arm",
	"freebsd/arm64",
	"illumos/amd64",
	"ios/amd64",
	"ios/arm64",
	"js/wasm",
	"linux/386",
	"linux/amd64",
	"linux/arm",
	"linux/arm64",
	"linux/loong64",
	"linux/mips",
	"linux/mips64",
	"linux/mips64le",
	"linux/mipsle",
	"linux/ppc64",
	"linux/ppc64le",
	"linux/riscv64",
	"linux/s390x",
	"netbsd/386",
	"netbsd/amd64",
	"netbsd/arm",
	"netbsd/arm64",
	"openbsd/386",
	"openbsd/amd64",
	"openbsd/arm",
	"openbsd/arm64",
	"openbsd/ppc64",
	"openbsd/riscv64",
	"plan9/386",
	"plan9/amd64",
	"plan9/arm",
	"solaris/amd64",
	"wasip1/wasm",
	"windows/386",
	"windows/amd64",
	"windows/arm64",
}

// knownGOOS and knownGOARCH are the values Go recognises in an implicit filename constraint,
// mirroring go/build's lists.
//
// They are deliberately a superset of knownPairs. A suffix the toolchain has dropped -- sparc,
// amd64p32, mips64p32 -- is still a suffix Go reads, so a file named for it is constrained to a
// configuration the matrix does not contain, and is therefore never built. That suppresses a
// diagnostic for a platform nobody can target, which is the safe direction. Deriving these lists
// from knownPairs instead leaves such a file unconstrained, reports it as built everywhere, and
// invents conflicts between it and ordinary files.
//
// The two must not be conflated: these decide what a name means, knownPairs decides what
// configurations are evaluated.
var (
	knownGOOS = []string{
		"aix", "android", "darwin", "dragonfly", "freebsd", "hurd", "illumos", "ios", "js",
		"linux", "nacl", "netbsd", "openbsd", "plan9", "solaris", "wasip1", "windows", "zos",
	}

	knownGOARCH = []string{
		"386", "amd64", "amd64p32", "arm", "arm64", "loong64", "mips", "mips64", "mips64le",
		"mips64p32", "mips64p32le", "mipsle", "ppc", "ppc64", "ppc64le", "riscv", "riscv64",
		"s390", "s390x", "sparc", "sparc64", "wasm",
	}

	knownGOOSSet   = toSet(knownGOOS)
	knownGOARCHSet = toSet(knownGOARCH)
)

func toSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, v := range values {
		set[v] = true
	}
	return set
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

// implicitConstraint returns the build constraint Go infers from a file's name alone, as a
// //go:build expression, or "" when the name implies none.
//
// Go applies two independent constraints to a file, and reading only the comment misses half the
// picture. syscall/env_windows.go carries no //go:build line at all -- it is Windows-only purely
// because of the _windows suffix. A checker that sees no comment treats it as built everywhere
// and collides it with every other declaration of the same name, which on the standard library
// produced 61,287 false positives in syscall alone.
//
// The rules follow go/build: strip a trailing _test, then split what remains on underscores.
// Two trailing parts may be GOOS, GOARCH, or GOOS_GOARCH; anything else implies no constraint.
//
// The result is written as bare tags -- "linux && amd64", not "GOOS=linux && GOARCH=amd64". Both
// spellings appear in the Go toolchain, but only the bare form is valid expression syntax:
// go/build/constraint reads "=" as an assignment, so "GOOS=linux" fails to parse. The evaluator
// here already resolves a bare tag against the configuration's GOOS and GOARCH.
//
// A file whose name begins with "_" or "." is ignored by the go tool. golens still outlines it --
// an agent asking what a directory contains wants all of it -- but it must not be treated as a
// build participant, so it yields no constraint here and is reported as always built for
// duplicate purposes, which is the conservative direction.
func implicitConstraint(path string) string {
	name := filepath.Base(path)

	// "_" and "." prefixed files are not build participants at all. They are still outlined.
	if strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".") {
		return ""
	}

	name = strings.TrimSuffix(name, ".go")

	// Everything before the first underscore is a package or symbol name, not a constraint:
	// "os/zero_copy_linux.go" is linux-only and "runtime/os_only_solaris.go" is solaris-only.
	// Dropping the prefix is what makes those work.
	i := strings.Index(name, "_")
	if i < 0 {
		return ""
	}
	parts := strings.Split(name[i:], "_")

	// A trailing _test names the file's role rather than a platform.
	if n := len(parts); n > 0 && parts[n-1] == "test" {
		parts = parts[:n-1]
	}

	n := len(parts)
	if n < 1 {
		return ""
	}

	// GOOS_GOARCH first, then either alone on the final component. Testing the arch before the
	// pair matched "amd64" in zerrors_darwin_amd64.go against every GOOS and produced 27,000
	// false positives; testing only the pair missed zero_copy_linux.go entirely, because the
	// name has four components. Both orders are needed, in this order.
	if n >= 2 && knownGOOSSet[parts[n-2]] && knownGOARCHSet[parts[n-1]] {
		return parts[n-2] + " && " + parts[n-1]
	}
	if knownGOOSSet[parts[n-1]] || knownGOARCHSet[parts[n-1]] {
		return parts[n-1]
	}
	return ""
}

// fileConstraint returns the expression governing whether a file is built, combining the
// constraint Go infers from the file's name, the file's //go:build line, and whether the file
// requires cgo.
//
// The three are independent and all apply. requiresCgo is set for a file importing "C": the go
// tool places such a file in CgoFiles rather than GoFiles, so it is excluded outright when cgo is
// disabled. crypto/internal/sysrand/internal/seccomp holds a cgo file with no tag at all beside
// one guarded by "//go:build !linux || !cgo"; without the cgo term both would be built on a
// cgo-less linux, and the package would not compile, which it does.
//
// Returning nil means the file is always built.
func fileConstraint(path, buildConstraint string, requiresCgo bool) constraint.Expr {
	var terms []string
	if implicit := implicitConstraint(path); implicit != "" {
		terms = append(terms, implicit)
	}
	if buildConstraint != "" {
		// Parenthesised so that an "||" inside the file's own expression cannot swallow the
		// terms joined after it.
		terms = append(terms, "("+buildConstraint+")")
	}
	if requiresCgo {
		terms = append(terms, "cgo")
	}
	if len(terms) == 0 {
		return nil
	}
	return parseBuildConstraint(strings.Join(terms, " && "))
}
