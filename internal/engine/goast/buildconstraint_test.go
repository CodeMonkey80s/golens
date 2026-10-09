package goast

import "testing"

// TestBuildConstraintText covers the source-level scan that feeds both the outline header and
// the duplicate-declaration check.
//
// The legacy form matters as much as the modern one: a repository on go 1.15 carries only
// "// +build", and a tool that reads neither would report every platform variant as conflicting.
func TestBuildConstraintText(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"untagged", "package p\n\nfunc F() {}\n", ""},
		{"go build form", "//go:build linux\n\npackage p\n", "linux"},
		{"legacy form", "// +build linux\n\npackage p\n", "linux"},
		{"both forms, go build wins", "//go:build linux\n// +build linux\n\npackage p\n", "linux"},
		{
			"legacy disjunction",
			"// +build linux darwin\n\npackage p\n",
			"linux || darwin",
		},
		{
			"legacy lines are ANDed",
			"// +build linux darwin\n// +build !windows\n\npackage p\n",
			"(linux || darwin) && !windows",
		},
		{
			"constraint after a comment is still found",
			"// Copyright.\n//go:build darwin\n\npackage p\n",
			"darwin",
		},
		{
			"constraint below the package clause is ignored",
			"package p\n\n//go:build linux\n",
			"",
		},
		{
			"comment mentioning a constraint is not one",
			"// this file is not linux-only\npackage p\n",
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildConstraintText([]byte(tt.src)); got != tt.want {
				t.Errorf("buildConstraintText = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestDocumentCarriesBuildConstraint checks the value reaches the document, since the outline
// header and the duplicate-declaration check both read it from there.
func TestDocumentCarriesBuildConstraint(t *testing.T) {
	engine := New()

	doc, err := engine.ParseSource("p.go", []byte("//go:build linux\n\npackage p\n\nfunc F() {}\n"))
	if err != nil {
		t.Fatalf("ParseSource: %v", err)
	}
	if doc.BuildConstraint != "linux" {
		t.Errorf("BuildConstraint = %q, want %q", doc.BuildConstraint, "linux")
	}

	plain, err := engine.ParseSource("p.go", []byte("package p\n\nfunc F() {}\n"))
	if err != nil {
		t.Fatalf("ParseSource: %v", err)
	}
	if plain.BuildConstraint != "" {
		t.Errorf("BuildConstraint = %q, want empty for an untagged file", plain.BuildConstraint)
	}
}
