package usecase

import "testing"

// TestEffectiveConstraintText pins the display form. It is separate from fileConstraint's
// evaluator form: redundant parentheses and duplicate terms are dropped, and a term containing
// "||" keeps the parentheses that preserve its meaning.
func TestEffectiveConstraintText(t *testing.T) {
	cases := []struct {
		path  string
		build string
		cgo   bool
		want  string
	}{
		{"a.go", "", false, ""},
		{"a_windows.go", "", false, "windows"},
		{"a_linux_amd64.go", "", false, "linux && amd64"},
		{"a_windows.go", "windows", false, "windows"},
		{"a_linux.go", "amd64", false, "linux && amd64"},
		{"a_linux.go", "linux && amd64", false, "linux && amd64"},
		{"a_linux.go", "linux || darwin", false, "linux && (linux || darwin)"},
		{"a.go", "cgo", true, "cgo"},
		{"a_linux.go", "", true, "linux && cgo"},
	}

	for _, tc := range cases {
		if got := effectiveConstraintText(tc.path, tc.build, tc.cgo); got != tc.want {
			t.Errorf("effectiveConstraintText(%q, %q, %v) = %q, want %q",
				tc.path, tc.build, tc.cgo, got, tc.want)
		}
	}
}
