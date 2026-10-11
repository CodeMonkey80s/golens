package cli_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestTemplatePointsAtHelpRatherThanRestatingIt keeps the template from teaching the conventions.
//
// AGENTS.template.md is copied into a user's repository, where it is read before the tool is ever
// run. While it restated the conventions -- which stream check writes, where a span starts, that a
// symbol can be ambiguous -- an agent holding it never needed `golens --help`, so every claim about
// whether the help text is sufficient was untestable. Two cold-start runs bracketed that: one where
// the template was present and traps 1-3 could not be scored, and one where it was absent and the
// agent never reached for the tool at all.
//
// The split is deliberate and this test holds it in place:
//
//	conventions -> --help. Facts about the tool; they change when the tool changes.
//	judgement   -> this file. When to use it and what a result proves; they do not.
//
// So the template may describe a *category* of hazard -- that offsets are counted in bytes, that
// one stream can look like a clean result -- but must not state the specific rule. An agent that
// reads this and then reads --help knows everything; an agent that reads only this does not, which
// is the point.
func TestTemplatePointsAtHelpRatherThanRestatingIt(t *testing.T) {
	if testing.Short() {
		t.Skip("reads the template; skipped by -short")
	}

	root := repoRoot(t)
	body := read(t, filepath.Join(root, "documentation", "AGENTS.template.md"))

	// Statements of the specific rule. Each is a fact an agent could act on without running
	// --help, which is exactly what must not happen.
	leaked := []struct {
		phrase string
		why    string
	}{
		{"start at a declaration", "the rule for where a span begins"},
		{"rather than its keyword", "the rule for where a span begins"},
		{"span starts at the declaration", "the rule for where a span begins"},
		{"writes to stderr", "which stream a command writes"},
		{"stdout alone", "which stream a command writes"},
		{"2>&1", "how to capture the stream a command writes"},
	}

	for _, l := range leaked {
		if strings.Contains(body, l.phrase) {
			t.Errorf("the template states %q (%s).\nIt must point at --help instead: an agent reading this should\n"+
				"still have to run the help to learn the rule. Categories of hazard are fine;\n"+
				"the specific convention is not.", l.phrase, l.why)
		}
	}

	// The pointer must survive. De-leaking is only correct if something replaces it.
	for _, required := range []string{
		"golens --help",
		"golens <command> --help",
		"Read that help before using it",
	} {
		if !strings.Contains(body, required) {
			t.Errorf("the template lost its pointer to the help text: %q is missing.\n"+
				"Without it an agent has no route to the conventions.", required)
		}
	}
}

// TestTemplateTreatsGolensAsOptional pins the availability rule.
//
// The rule is that the agent uses golens if it is on PATH and otherwise works normally. It is not
// the agent's to install, build or fetch: that is the user's decision, and an agent that tries to
// satisfy itself a missing dependency is doing something surprising and slow.
//
// This matters beyond style. A template that says "stop and say so rather than substituting grep"
// makes an agent refuse to work in a repository whose owner never intended golens to be used there.
func TestTemplateTreatsGolensAsOptional(t *testing.T) {
	if testing.Short() {
		t.Skip("reads the template; skipped by -short")
	}

	root := repoRoot(t)
	body := read(t, filepath.Join(root, "documentation", "AGENTS.template.md"))

	if !strings.Contains(body, "if it is not on `PATH`") && !strings.Contains(body, "not on `PATH`") {
		t.Error("the template does not say what to do when golens is absent from PATH")
	}

	// The old instruction was to stop. That is the opposite of proceeding normally.
	for _, forbidden := range []string{
		"stop and say so",
		"it is not installed here",
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("the template still contains %q. golens is optional: an agent that finds it "+
				"missing should work normally, not refuse the task.", forbidden)
		}
	}

	// Nor should it try to acquire the tool.
	for _, forbidden := range []string{
		"go install",
		"go get",
		"git clone",
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("the template tells the agent to obtain golens (%q). Presence is the user's "+
				"decision; the agent uses what is on PATH.", forbidden)
		}
	}

	// The conditional must actually be present, not just the prohibitions.
	if !strings.Contains(body, "work normally") {
		t.Error("the template must tell the agent to work normally when golens is absent")
	}
}
