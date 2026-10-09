package goast

import (
	"go/build/constraint"
	"strings"
)

// buildConstraintText returns a file's build constraint normalised to //go:build syntax, or
// the empty string when the file is always built.
//
// Both source syntaxes are recognised. The //go:build form is preferred, matching the
// toolchain; the legacy // +build form is translated because a repository may carry either or
// both, and legacy lines are combined with AND, since each line is an OR-list of tags and
// consecutive lines all have to hold.
//
// Lines are passed through untrimmed. constraint.Parse rejects a line with leading whitespace,
// which is how the toolchain reads it too, so trimming here would accept files the compiler
// rejects.
//
// A constraint that fails to parse yields the empty string, meaning "always built". That keeps
// the conservative behaviour: an unrecognised file is treated as unconstrained rather than
// silently excluded.
func buildConstraintText(src []byte) string {
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

	switch {
	case goBuild != nil:
		return goBuild.String()
	case plusBuild != nil:
		return plusBuild.String()
	}
	return ""
}
