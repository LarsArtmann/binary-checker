// Package gittest builds throwaway git repositories for tests.
//
// Every package in this module exercises behaviour defined over git's index, so
// each of them needs the same fixture setup. Keeping it here means the git
// invocation, the isolated global config, and the file-writing conventions are
// identical everywhere instead of drifting per package.
package gittest

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// NewRepo creates an empty git repository in a temporary directory and returns
// its path.
//
// The repository is isolated from the developer's global and system git config
// so a local alias, hook, or signing requirement cannot change test outcomes.
func NewRepo(t *testing.T) string {
	t.Helper()

	emptyConfig := filepath.Join(t.TempDir(), "gitconfig")

	t.Setenv("GIT_CONFIG_GLOBAL", emptyConfig)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	dir := t.TempDir()

	Run(t, dir, "init", "-q")
	Run(t, dir, "config", "user.email", "test@example.com")
	Run(t, dir, "config", "user.name", "Test")

	return dir
}

// Run executes a git command inside dir and fails the test on a non-zero exit.
func Run(t *testing.T, dir string, args ...string) {
	t.Helper()

	// #nosec G204 -- args are test-authored literals, never user input.
	command := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir}, args...)...)

	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

// WriteFile writes data to the repository-relative path rel inside dir,
// creating parent directories as needed.
func WriteFile(t *testing.T, dir, rel string, data []byte) {
	t.Helper()

	absolute := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(absolute, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// AddFiles stages the named files, including ones a .gitignore would normally
// exclude, so a test can create the tracked-and-ignored case on purpose.
func AddFiles(t *testing.T, dir string, rels ...string) {
	t.Helper()

	Run(t, dir, append([]string{"add", "-f"}, rels...)...)
}
