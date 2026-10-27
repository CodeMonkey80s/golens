package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type jsonErrorEnvelope struct {
	SchemaVersion int `json:"schema_version"`
	Error         struct {
		Kind       string   `json:"kind"`
		Message    string   `json:"message"`
		ExitCode   int      `json:"exit_code"`
		Candidates []string `json:"candidates"`
		Skipped    int      `json:"skipped"`
		Total      int      `json:"total"`
	} `json:"error"`
}

func parseJSONError(t *testing.T, stderr string) jsonErrorEnvelope {
	t.Helper()
	var env jsonErrorEnvelope
	if err := json.Unmarshal([]byte(stderr), &env); err != nil {
		t.Fatalf("stderr is not a JSON error envelope: %v\n%s", err, stderr)
	}
	if env.SchemaVersion != 1 {
		t.Errorf("schema_version = %d, want 1", env.SchemaVersion)
	}
	return env
}

func TestJSONErrors(t *testing.T) {
	t.Run("not found", func(t *testing.T) {
		dir := t.TempDir()
		_, stderr, code := run(t, "get", "Nope", dir, "--json-errors")
		if code != 2 {
			t.Fatalf("exit = %d, want 2", code)
		}
		env := parseJSONError(t, stderr)
		if env.Error.Kind != "not_found" {
			t.Errorf("kind = %q, want not_found", env.Error.Kind)
		}
		if env.Error.ExitCode != 2 {
			t.Errorf("exit_code = %d, want 2", env.Error.ExitCode)
		}
	})

	t.Run("ambiguous carries candidates", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "a.go", "package p\n\nfunc F() {}\n")
		writeFile(t, dir, "b.go", "package p\n\nfunc F() {}\n")

		_, stderr, code := run(t, "get", "F", dir, "--json-errors")
		if code != 2 {
			t.Fatalf("exit = %d, want 2", code)
		}
		env := parseJSONError(t, stderr)
		if env.Error.Kind != "ambiguous" {
			t.Errorf("kind = %q, want ambiguous", env.Error.Kind)
		}
		if len(env.Error.Candidates) != 2 {
			t.Errorf("candidates = %v, want 2", env.Error.Candidates)
		}
	})

	t.Run("check diagnostics keep stdout payload only", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "a.go", "package p\n\nimport \"fmt\"\n\nfunc F() {}\n")

		stdout, stderr, code := run(t, "check", dir, "--json", "--json-errors")
		if code != 3 {
			t.Fatalf("exit = %d, want 3", code)
		}
		if stderr != "" {
			t.Errorf("stderr = %q, want empty: diagnostics payload is stdout", stderr)
		}
		if !strings.Contains(stdout, "\"diagnostics\"") {
			t.Errorf("stdout does not carry the diagnostics payload:\n%s", stdout)
		}
	})

	t.Run("partial outline keeps stdout valid and errors on stderr", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "a.go", "package p\n\nfunc A() {}\n")
		writeFile(t, dir, "b.go", "package p\n\nfunc B( {\n")

		stdout, stderr, code := run(t, "outline", dir, "--json", "--json-errors")
		if code != 4 {
			t.Fatalf("exit = %d, want 4", code)
		}
		var payload []map[string]any
		if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
			t.Fatalf("stdout is not valid JSON after a partial run: %v\n%s", err, stdout)
		}
		env := parseJSONError(t, stderr)
		if env.Error.Kind != "parse" {
			t.Errorf("kind = %q, want parse", env.Error.Kind)
		}
		if env.Error.Skipped != 1 || env.Error.Total != 2 {
			t.Errorf("skipped/total = %d/%d, want 1/2", env.Error.Skipped, env.Error.Total)
		}
	})

	t.Run("unreadable file is an IO error", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "a.go")
		writeFile(t, dir, "a.go", "package p\n\nfunc A() {}\n")
		if err := os.Chmod(path, 0o000); err != nil {
			t.Skipf("cannot make the file unreadable: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

		stdout, stderr, code := run(t, "check", dir, "--json", "--json-errors")
		if code != 4 {
			t.Fatalf("exit = %d, want 4", code)
		}
		var diagnostics map[string]any
		if err := json.Unmarshal([]byte(stdout), &diagnostics); err != nil {
			t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout)
		}
		env := parseJSONError(t, stderr)
		if env.Error.Kind != "io" {
			t.Errorf("kind = %q, want io", env.Error.Kind)
		}
		if env.Error.Skipped != 1 {
			t.Errorf("skipped = %d, want 1", env.Error.Skipped)
		}
	})
}
