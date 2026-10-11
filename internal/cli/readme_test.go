package cli_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestReadmeExamplesAreReproducible keeps the README's console examples true.
//
// Every claim in the documentation was checked against the binary by hand, twice, and both times a
// block had drifted: the outline example listed six types where the file had eight, at stale line
// numbers, and two stripped binary sizes disagreed with each other in one file. Hand-checking is
// what found those. It is also what failed to keep them current, because the cost is only paid when
// someone remembers to re-run it.
//
// So this test pays the cost on every `go test`. It builds the binary and re-runs the README's
// console blocks that name a file in this repository, comparing the recorded output to what the
// tool produces now.
//
// SCOPE. Only blocks whose command targets a real file in this repo are checked. Blocks referring to
// the specification's fictional `user.go` cannot be executed and are marked ILLUSTRATIVE below, so
// nobody mistakes an unchecked example for a verified one. This is deliberately narrow: prose,
// rationale and history are not machine-checkable and must not be generated. What is machine-checkable
// is the *output*, and that is all this covers.
func TestReadmeExamplesAreReproducible(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary and walks the README; skipped by -short")
	}

	root := repoRoot(t)
	readme := read(t, filepath.Join(root, "README.md"))
	bin := buildBinary(t, root)

	// Each entry is a command from the README whose output is quoted below it. The three
	// reproducible examples are the ones naming a file in this repository; the rest reference
	// user.go, which does not exist, and are left as illustrations.
	reproducible := []struct {
		command string
		reason  string
	}{
		{"outline internal/domain/domain.go --depth 1", "the headline example"},
		{"get Bounds.ByteLen internal/domain/domain.go", "the byte-offset example"},
		{"bounds Bounds.ByteLen internal/domain/domain.go", "the one-line bounds example"},
	}

	for _, ex := range reproducible {
		t.Run(ex.command, func(t *testing.T) {
			want := blockAfter(t, readme, "$ golens "+ex.command)
			if strings.TrimSpace(want) == "" {
				t.Fatalf("README has no recorded output for %q (%s); add it or drop the example", ex.command, ex.reason)
			}

			got := runBinary(t, bin, root, strings.Fields(ex.command)...)
			if normalise(got) != normalise(want) {
				t.Errorf("README output for %q is stale.\nThe README claims:\n%s\nThe tool produces:\n%s\n\n"+
					"Re-run the command and paste the real output, or delete the example.", ex.command, want, got)
			}
		})
	}
}

// TestReadmeMarksItsUnverifiedExamples records which console blocks no test checks.
//
// A block that looks verified but is not is worse than one that is obviously an illustration,
// because a reader trusts it in the same way. Every golens command in the README is classified
// here: reproduced above, or explicitly marked as targeting the specification's sample file.
func TestReadmeMarksItsUnverifiedExamples(t *testing.T) {
	if testing.Short() {
		t.Skip("reads the README; skipped by -short")
	}

	root := repoRoot(t)
	readme := read(t, filepath.Join(root, "README.md"))

	command := regexp.MustCompile(`(?m)^\$ (golens .*)$`)
	verified := map[string]bool{
		"golens outline internal/domain/domain.go --depth 1":     true,
		"golens get Bounds.ByteLen internal/domain/domain.go":    true,
		"golens bounds Bounds.ByteLen internal/domain/domain.go": true,
		"golens check ./internal":                                true,
	}

	var unclassified []string
	for _, m := range command.FindAllStringSubmatch(readme, -1) {
		cmd := strings.TrimSpace(m[1])
		// Strip a trailing comment or argument list that is illustrative.
		key := cmd
		if i := strings.Index(key, " <"); i >= 0 {
			key = key[:i]
		}
		if verified[key] {
			continue
		}
		// Everything else must mention the specification's sample file, so a reader can see
		// at a glance that it is illustrative rather than reproducible.
		if strings.Contains(cmd, "user.go") || strings.Contains(cmd, "<") || strings.Contains(cmd, "<Symbol>") ||
			strings.Contains(cmd, "<path>") || strings.Contains(cmd, "<Type>") || strings.Contains(cmd, "<MethodName>") ||
			strings.Contains(cmd, "<file.go>") {
			continue
		}
		unclassified = append(unclassified, cmd)
	}

	if len(unclassified) > 0 {
		t.Errorf("README has console examples that are neither reproduced by a test nor marked "+
			"as targeting the specification's sample file:\n  %s\n\n"+
			"Either make the example reproducible (name a real file in this repository) or mark it "+
			"as illustrative, so a reader can tell which claims are checked.",
			strings.Join(unclassified, "\n  "))
	}
}

// blockAfter returns the text under a "$ command" line in a fenced console block, up to the closing
// fence or the next prompt.
func blockAfter(t *testing.T, doc, prompt string) string {
	t.Helper()

	i := strings.Index(doc, prompt)
	if i < 0 {
		t.Fatalf("README does not contain %q", prompt)
	}
	rest := doc[i+len(prompt):]

	// Consume the rest of the prompt line.
	if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
		rest = rest[nl+1:]
	} else {
		return ""
	}

	var out []string
	for _, line := range strings.Split(rest, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(line, "```") {
			break // end of the fence
		}
		if strings.HasPrefix(trimmed, "$ ") || strings.HasPrefix(trimmed, "# ") {
			break // the next command or a comment: output ended
		}
		out = append(out, line)
	}

	// Trailing blank lines are not part of the recorded output.
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	return strings.Join(out, "\n")
}

func normalise(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
	}
	return strings.Join(lines, "\n")
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", path, err)
	}
	return string(b)
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("no go.mod at %s: %v", root, err)
	}
	return root
}

// buildBinary compiles the CLI once for the whole test, so the examples are checked against the
// same build the reader would produce.
func buildBinary(t *testing.T, root string) string {
	t.Helper()

	bin := filepath.Join(t.TempDir(), "golens")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/golens")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

func runBinary(t *testing.T, bin, dir string, args ...string) string {
	t.Helper()

	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// A non-zero exit is fine for some examples; the output is what is being compared.
		var exitErr *exec.ExitError
		if !errorsAs(err, &exitErr) {
			t.Fatalf("running %v: %v\n%s", args, err, stderr.String())
		}
	}
	return stdout.String()
}

func errorsAs(err error, target **exec.ExitError) bool {
	e, ok := err.(*exec.ExitError)
	if ok {
		*target = e
	}
	return ok
}
