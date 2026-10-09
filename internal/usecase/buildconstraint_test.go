package usecase

import "testing"

// TestBuildReleaseTags pins the version parsing, including the prerelease forms that a
// developer toolchain reports. Getting these wrong makes a "//go:build go1.21" file look
// unbuildable, which suppresses a redeclaration the compiler would report.
func TestBuildReleaseTags(t *testing.T) {
	tests := []struct {
		version string
		want    int
	}{
		{"go1.27.2", 27},
		{"go1.27.2-X:nodwarf5 linux/amd64", 27},
		{"go1.21rc1", 21},
		{"go1.20", 20},
		{"go2.0.1", 0}, // major 2 is not a go1.N tag; treated as unparseable
		{"devel go1.28-abc", 0},
		{"gopher", 0},
		{"", 0},
	}

	for _, tt := range tests {
		tags := buildReleaseTags(tt.version)
		if len(tags) != tt.want {
			t.Errorf("buildReleaseTags(%q) gave %d tags, want %d", tt.version, len(tags), tt.want)
		}
		if tt.want > 0 {
			if !tags["go1.1"] {
				t.Errorf("buildReleaseTags(%q) is missing go1.1", tt.version)
			}
			if tags["go1.99"] {
				t.Errorf("buildReleaseTags(%q) should not contain go1.99", tt.version)
			}
		}
	}
}
