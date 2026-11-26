package binarycheck

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	errorfamily "github.com/larsartmann/go-error-family"

	"github.com/larsartmann/binary-checker/internal/gittest"
)

// BC003 answers a different question than BC001 and BC002, so a file may
// offend against both. Neither rule may hide the other: otherwise
// `--disable BC002 --enable BC003` drops a tracked-and-ignored artifact from
// the report entirely.
func TestScanReportsTrackedIgnoredNextToOtherRules(t *testing.T) {
	t.Parallel()

	dir := gittest.NewRepo(t)

	gittest.WriteFile(t, dir, ".gitignore", []byte("*.log\n*.zip\n"))
	gittest.WriteFile(t, dir, "debug.log", []byte{0x00, 0x01})
	gittest.WriteFile(t, dir, "bundle.zip", []byte("not really a zip"))
	gittest.AddFiles(t, dir, ".gitignore", "debug.log", "bundle.zip")

	result := scanRepo(t, dir)

	for _, test := range []struct {
		rule  RuleID
		files []string
	}{
		{rule: RuleTrackedBinary, files: []string{"debug.log"}},
		{rule: RuleTrackedPattern, files: []string{"bundle.zip"}},
		{rule: RuleTrackedIgnored, files: []string{"bundle.zip", "debug.log"}},
	} {
		var flagged []string

		for _, item := range findingsByRule(result, test.rule) {
			flagged = append(flagged, string(item.Position.File))
		}

		if len(flagged) != len(test.files) {
			t.Fatalf("%s flagged %v, want %v", test.rule, flagged, test.files)
		}
	}
}

// A cancelled scan must fail. Reporting the un-inspected remainder as
// "skipped" would read as a cleaner repository than the scan can honestly
// claim.
func TestScanFailsOnCancelledContext(t *testing.T) {
	t.Parallel()

	dir := gittest.NewRepo(t)

	gittest.WriteFile(t, dir, "blob.bin", []byte{0x00})
	gittest.AddFiles(t, dir, "blob.bin")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := NewChecker().Scan(ctx, dir)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want one wrapping context.Canceled", err)
	}

	// exec already reports the context error in its chain, so the chain alone
	// cannot tell "interrupted" from "git failed": the classification must.
	if errorfamily.Code(err) != codeGitCancelled {
		t.Fatalf("code = %q, want %q (error: %v)", errorfamily.Code(err), codeGitCancelled, err)
	}

	if result != nil {
		t.Fatalf("a cancelled scan must not return a result, got %+v", result)
	}
}

// inspectAll is where a deadline that expires after the git queries would
// otherwise turn every remaining file into a silent "skip".
func TestInspectAllFailsOnCancelledContext(t *testing.T) {
	t.Parallel()

	dir := gittest.NewRepo(t)

	gittest.WriteFile(t, dir, "blob.bin", []byte{0x00})
	gittest.AddFiles(t, dir, "blob.bin")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	inspections, err := inspectAll(ctx, dir, []trackedFile{{Path: "blob.bin"}}, Config{}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want one wrapping context.Canceled", err)
	}

	if inspections != nil {
		t.Fatalf("a cancelled inspection pass must not return inspections, got %+v", inspections)
	}
}

// "exit status 128" alone does not tell a user what to fix. git's own stderr
// does, so it travels with the error, together with the query that failed.
func TestGitFailureExplainsItselfWithStderr(t *testing.T) {
	t.Parallel()

	_, err := discoverGitRoot(context.Background(), t.TempDir())
	if !errors.Is(err, ErrNotGitRepository) {
		t.Fatalf("error = %v, want one matching ErrNotGitRepository", err)
	}

	for _, want := range []string{"git rev-parse --show-toplevel", "fatal:", "not a git repository"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not explain itself: missing %q", err, want)
		}
	}
}

// A failed LFS attribute lookup must not pass for "no LFS files here": the
// scan would report LFS pointers as binary and nobody would learn why.
func TestLFSPathsReportsFailures(t *testing.T) {
	t.Parallel()

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	lfs, err := (&gitClient{root: gittest.NewRepo(t)}).lfsPaths(cancelled, []string{"source.psd"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want one wrapping context.Canceled", err)
	}

	if errorfamily.Code(err) != codeGitCancelled {
		t.Fatalf("code = %q, want %q (error: %v)", errorfamily.Code(err), codeGitCancelled, err)
	}

	if lfs != nil {
		t.Fatalf("a cancelled lookup must not return a partial map, got %v", lfs)
	}

	// A root that is not a repository makes git fail outright.
	_, err = (&gitClient{root: t.TempDir()}).lfsPaths(context.Background(), []string{"source.psd"})
	if errorfamily.Code(err) != codeGitFailed {
		t.Fatalf("code = %q, want %q (error: %v)", errorfamily.Code(err), codeGitFailed, err)
	}

	if errorfamily.Classify(err) != errorfamily.Infrastructure {
		t.Fatalf("a git that cannot answer is an infrastructure problem, got %v", errorfamily.Classify(err))
	}
}

// BC003 is defined over the repository's .gitignore files. A rule that only
// exists in a personal ignore file (.git/info/exclude, the user's global
// ignore file) must not turn into a finding, or the same commit would be
// clean on one machine and flagged on the next.
func TestScanIgnoresPersonalIgnoreRules(t *testing.T) {
	personal := t.TempDir()

	if err := os.MkdirAll(filepath.Join(personal, "git"), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(personal, "git", "ignore"), []byte("*.zip\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// t.Setenv is process-wide, which is exactly what the scanner's own git
	// child processes inherit; this test therefore cannot run in parallel.
	t.Setenv("XDG_CONFIG_HOME", personal)

	dir := gittest.NewRepo(t)

	gittest.WriteFile(t, dir, "bundle.zip", []byte("not really a zip"))
	gittest.WriteFile(t, dir, "notes.log", []byte("log line\n"))
	gittest.WriteFile(t, dir, ".git/info/exclude", []byte("*.log\n"))
	gittest.AddFiles(t, dir, "bundle.zip", "notes.log")

	result := scanRepo(t, dir)

	if ignored := findingsByRule(result, RuleTrackedIgnored); len(ignored) != 0 {
		t.Fatalf("personal ignore rules must not produce BC003 findings, got %d", len(ignored))
	}
}
