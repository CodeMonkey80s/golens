// Package testfixture holds Go source fixtures shared by several test suites.
//
// The sources live here as string constants rather than as testdata/ files so they can be
// written to a temp directory by any suite, at any depth, without touching the repository
// tree. Classes that need a stable path for golden comparison should use testdata/ instead.
//
// Line numbers in these fixtures are load-bearing: golden outline output depends on them, so
// inserting a blank line here will break those tests. That is deliberate, because it makes an
// accidental layout change visible rather than silent.
package testfixture

import (
	"os"
	"path/filepath"
	"strings"
)

// Store is the canonical multi-declaration fixture used by several test suites.
//
// It exercises the shapes the outline must render: imports, an exported and an unexported
// type, a one-line and a multi-line declaration, a package-level func, a value receiver
// method and a pointer receiver method. Line numbers are fixed because golden output
// depends on them.
const Store = `package store

import (
	"context"
	"fmt"
)

type User struct {
	Id   int
	Name string
}

type UserStore struct {
	mu    sync.Mutex
	cache map[int]*User
}

type Alias = User

var ErrNotFound = errors.New("not found")

const MaxUsers = 100

func NewUserStore() *UserStore {
	return &UserStore{}
}

func (s *UserStore) GetUser(ctx context.Context, id int) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u, ok := s.cache[id]; ok {
		return u, nil
	}
	return nil, ErrNotFound
}

func (s UserStore) Len() int {
	return len(s.cache)
}
`

// Unexported covers class 2: identifiers that are not exported but must still resolve.
const Unexported = `package hidden

type inner struct {
	value int
}

var counter int

func bump() {
	counter++
}

func (i *inner) get() int {
	return i.value
}
`

// Ambiguous covers class 3: two receivers sharing a method name, which must exit 2 with
// both candidates rather than guessing.
const Ambiguous = `package coll

type Alpha struct{}

type Beta struct{}

func (Alpha) Save() {}
func (Beta) Save() {}
`

// ImportVariety covers class 4: a named import, a blank import and a dot import alongside
// ordinary ones. Only the named import may be reported unused.
const ImportVariety = `package imports

import (
	"context"
	jsonalias "encoding/json"
	_ "net/http/pprof"
	. "strings"
)

func F() string {
	b, _ := jsonalias.Marshal(1)
	return context.Background().String() + string(b) + ToUpper("x")
}
`

// UnusedImport covers class 4's negative case: a file whose only import is unused.
const UnusedImport = `package lonely

import "fmt"

func F() int {
	return 1
}
`

// Duplicate covers class 3's other half: a redeclared top-level name.
const Duplicate = `package dup

func F() {}

func F() {}
`

// Unicode covers class 7: multi-byte identifiers, where a rune-based offset
// implementation would report the wrong byte range.
const Unicode = `package café

func Ünïcödé(naïve string) string {
	return naïve + "日本語"
}
`

// Empty is class 9: a file with a package clause and nothing else.
const Empty = "package empty\n"

// CommentsOnly is class 10: a valid file with no declarations.
const CommentsOnly = `// Package comments has nothing to declare.
package comments

// A trailing comment.
`

// Broken is class 11: a syntax error, which must exit 4 for get and 3 for check.
const Broken = `package broken

func f( {
`

// Crlf is class 5: CRLF line endings. go/token counts the CR as part of the line, so byte
// offsets shift by one per line while the half-open invariant must still hold.
const Crlf = "package crlf\r\n\r\nfunc f() {\r\n}\r\n"

// Bom is class 6: a UTF-8 byte order mark. The BOM occupies three bytes before the package
// clause, so every offset in the file is shifted.
const Bom = "\xef\xbb\xbfpackage bom\n\nfunc f() {}\n"

// Write materialises src at dir/name, creating parent directories as needed.
//
// name may contain slashes to build a nested tree, which is how class 12's deep directory
// fixture is expressed.
func Write(t testingT, dir, name, src string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
	return path
}

// testingT is the subset of testing.TB these helpers need. Declaring it locally keeps the
// fixture package importable from both internal and external test packages.
type testingT interface {
	Helper()
	Fatalf(format string, args ...any)
}

// HasGoSuffix reports whether name looks like a Go source file.
func HasGoSuffix(name string) bool { return strings.HasSuffix(name, ".go") }
