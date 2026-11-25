package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/larsartmann/binary-checker/internal/gittest"
)

// Exit codes are a contract with CI: 0, 1, and 2 are verdicts about findings,
// and anything else means no verdict was delivered. A triage-only run and a
// failed run must never share a code.
func TestRunKeepsVerdictAndOperationalExitCodesApart(t *testing.T) {
	t.Parallel()

	dir := gittest.NewRepo(t)
	gittest.WriteFile(t, dir, "archive.zip", []byte("not really a zip"))
	gittest.AddFiles(t, dir, "archive.zip")

	if code, _, _ := runCLI(t, dir); code != exitTriage {
		t.Fatalf("triage-only exit = %d, want %d", code, exitTriage)
	}

	if code, _, _ := runCLI(t, t.TempDir()); code != exitUsage {
		t.Fatalf("exit outside a repository = %d, want %d", code, exitUsage)
	}

	if code, _, stderr := runCLI(t, "--format", "yaml", dir); code != exitUsage {
		t.Fatalf("exit for an unknown format = %d, want %d (stderr: %s)", code, exitUsage, stderr)
	}

	if code, _, stderr := runCLI(t, "--min-confidence", "certain", dir); code != exitUsage {
		t.Fatalf("exit for a bad threshold = %d, want %d (stderr: %s)", code, exitUsage, stderr)
	}

	if code, _, stderr := runCLI(t, dir, dir); code != exitUsage {
		t.Fatalf("exit for two directories = %d, want %d (stderr: %s)", code, exitUsage, stderr)
	}
}

// Disabling a rule removes that rule's findings and nothing else. BC003 must
// survive `--disable BC002`, or a tracked-and-ignored artifact would vanish
// from a report the user asked for.
func TestRunDisabledRuleHidesOnlyItsOwnFindings(t *testing.T) {
	t.Parallel()

	dir := gittest.NewRepo(t)
	gittest.WriteFile(t, dir, ".gitignore", []byte("*.zip\n"))
	gittest.WriteFile(t, dir, "bundle.zip", []byte("not really a zip"))
	gittest.AddFiles(t, dir, ".gitignore", "bundle.zip")

	code, stdout, stderr := runCLI(t, "--enable", "BC003", "--disable", "BC002", dir)
	if code != exitFindings {
		t.Fatalf("exit = %d, want %d (stderr: %s)", code, exitFindings, stderr)
	}

	if !strings.Contains(stdout, "BC003") || strings.Contains(stdout, "BC002") {
		t.Fatalf("stdout must carry BC003 alone: %s", stdout)
	}
}

// --list is informational, but an unknown rule named next to it is still an
// error: silently ignoring a typo is how a rule never gets enabled.
func TestRunListRejectsUnknownRule(t *testing.T) {
	t.Parallel()

	code, stdout, stderr := runCLI(t, "--list", "--enable", "BC999")
	if code != exitUsage {
		t.Fatalf("exit = %d, want %d (stderr: %s)", code, exitUsage, stderr)
	}

	if strings.Contains(stdout, "ID ") {
		t.Fatalf("--list must not print a rule table for an invalid invocation: %s", stdout)
	}
}

func TestRunPrintsVersion(t *testing.T) {
	t.Parallel()

	code, stdout, stderr := runCLI(t, "--version")
	if code != exitClean {
		t.Fatalf("exit = %d, want %d (stderr: %s)", code, exitClean, stderr)
	}

	if !strings.Contains(stdout, "binary-checker "+version) {
		t.Fatalf("stdout = %q, want the version %q", stdout, version)
	}
}

// A baseline with a typo in a key would parse as an empty baseline and fail
// every previously accepted finding. It must be rejected instead.
func TestRunRejectsBaselineWithUnknownMember(t *testing.T) {
	t.Parallel()

	dir := gittest.NewRepo(t)
	baseline := filepath.Join(t.TempDir(), "baseline.json")

	if err := os.WriteFile(baseline, []byte(`{"version":1,"tool":"binary-check","findngs":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	code, _, stderr := runCLI(t, "--baseline", baseline, dir)
	if code != exitUsage {
		t.Fatalf("exit = %d, want %d (stderr: %s)", code, exitUsage, stderr)
	}

	if !strings.Contains(stderr, baseline) {
		t.Fatalf("stderr must name the baseline file: %s", stderr)
	}
}

// A config error names the file to edit and the entry to fix.
func TestRunNamesConfigFileInConfigError(t *testing.T) {
	t.Parallel()

	dir := gittest.NewRepo(t)
	gittest.WriteFile(t, dir, ".binary-checker.json", []byte(`{"ignore":[{"path":"x"}]}`))
	gittest.AddFiles(t, dir, ".binary-checker.json")

	code, _, stderr := runCLI(t, dir)
	if code != exitUsage {
		t.Fatalf("exit = %d, want %d (stderr: %s)", code, exitUsage, stderr)
	}

	for _, want := range []string{".binary-checker.json", "ignore[0]", "reason"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr must mention %q: %s", want, stderr)
		}
	}
}
