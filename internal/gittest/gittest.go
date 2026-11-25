// Package gittest builds throwaway git repositories for tests.
//
// Every package in this module exercises behaviour defined over git's index, so
// each of them needs the same fixture setup. Keeping it here means the git
// invocation, the isolated config, and the file-writing conventions are
// identical everywhere instead of drifting per package.
//
// Isolation is per-command through the environment of each child process, never
// through t.Setenv. That is what lets the calling test call t.Parallel: a
// process-global mutation would make the whole suite serial.
package gittest

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// isolatedConfigName is the empty git config file each fixture repository gets.
// It lives inside .git, so git itself never sees it as a worktree file.
const isolatedConfigName = "test-global-config"

// NewRepo creates an empty git repository in a temporary directory and returns
// its path.
//
// The repository never reads the developer's global or system git config, nor
// the global ignore and attributes files git keeps under $XDG_CONFIG_HOME, so
// a local alias, hook, signing requirement, or "*.zip" ignore rule cannot
// change test outcomes.
func NewRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	Run(t, dir, "init", "-q")

	writeEmpty(t, filepath.Join(dir, ".git", isolatedConfigName))

	Run(t, dir, "config", "user.email", "test@example.com")
	Run(t, dir, "config", "user.name", "Test")

	return dir
}

// Run executes a git command inside dir and fails the test on a non-zero exit.
func Run(t *testing.T, dir string, args ...string) {
	t.Helper()

	// #nosec G204 -- args are test-authored literals, never user input.
	command := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir}, args...)...)
	command.Env = isolatedEnv(dir)

	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

// isolatedEnv is this process's environment plus git config isolation for dir.
//
// Silencing the config files is not enough: git also reads the ignore file at
// $XDG_CONFIG_HOME/git/ignore (and the global attributes file next to it)
// regardless of GIT_CONFIG_GLOBAL. Pointing XDG_CONFIG_HOME at a directory
// inside the fixture closes that leak, so a developer's "*.zip" ignore rule
// cannot turn a fixture into a tracked-but-ignored case.
func isolatedEnv(dir string) []string {
	configPath := filepath.Join(dir, ".git", isolatedConfigName)
	if _, err := os.Stat(configPath); err != nil {
		// Before the fixture file exists (git init itself), point git at the
		// null device: an empty global config by definition.
		configPath = os.DevNull
	}

	return append(
		os.Environ(),
		"GIT_CONFIG_GLOBAL="+configPath,
		"GIT_CONFIG_NOSYSTEM=1",
		"XDG_CONFIG_HOME="+filepath.Join(dir, ".git"),
	)
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

func writeEmpty(t *testing.T, path string) {
	t.Helper()

	file, err := os.Create(path) // #nosec G304 -- path is built from t.TempDir.
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}

	if err := file.Close(); err != nil {
		t.Fatalf("close %s: %v", path, err)
	}
}
