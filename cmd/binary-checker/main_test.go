package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func newGitRepo(t *testing.T) string {
	t.Helper()

	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	dir := t.TempDir()

	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "config", "user.email", "test@example.com")
	gitRun(t, dir, "config", "user.name", "Test")

	return dir
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()

	command := exec.Command("git", append([]string{"-C", dir}, args...)...)

	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

func writeRepoFile(t *testing.T, dir, rel string, data []byte) {
	t.Helper()

	absolute := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(absolute, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func addRepoFiles(t *testing.T, dir string, rels ...string) {
	t.Helper()

	gitRun(t, dir, append([]string{"add", "-f"}, rels...)...)
}

func runCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()

	var stdout, stderr bytes.Buffer

	code := run(args, &stdout, &stderr)

	return code, stdout.String(), stderr.String()
}

func TestRunReportsActionableFindings(t *testing.T) {
	dir := newGitRepo(t)
	writeRepoFile(t, dir, "blob.bin", []byte{0x00, 0x01})
	addRepoFiles(t, dir, "blob.bin")

	code, stdout, stderr := runCLI(t, dir)
	if code != exitFindings {
		t.Fatalf("exit = %d, want %d (stderr: %s)", code, exitFindings, stderr)
	}

	if !strings.Contains(stdout, "BC001") || !strings.Contains(stdout, "blob.bin") {
		t.Fatalf("stdout missing finding: %s", stdout)
	}
}

func TestRunTriageExitCodeForLowConfidenceFindings(t *testing.T) {
	dir := newGitRepo(t)
	writeRepoFile(t, dir, "archive.zip", []byte("not a zip"))
	addRepoFiles(t, dir, "archive.zip")

	code, stdout, _ := runCLI(t, dir)
	if code != exitTriage {
		t.Fatalf("exit = %d, want %d", code, exitTriage)
	}

	if !strings.Contains(stdout, "TRIAGE") || !strings.Contains(stdout, "BC002") {
		t.Fatalf("stdout missing triage finding: %s", stdout)
	}
}

func TestRunJSONFormat(t *testing.T) {
	dir := newGitRepo(t)
	writeRepoFile(t, dir, "blob.bin", []byte{0x00, 0x01})
	addRepoFiles(t, dir, "blob.bin")

	code, stdout, stderr := runCLI(t, "--format", "json", dir)
	if code != exitFindings {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}

	var report jsonReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout)
	}

	if report.Tool != "binary-check" || len(report.Actionable) != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}

	if report.FilesScanned != 1 {
		t.Fatalf("filesScanned = %d, want 1", report.FilesScanned)
	}
}

func TestRunSARIFFormat(t *testing.T) {
	dir := newGitRepo(t)
	writeRepoFile(t, dir, "blob.bin", []byte{0x00, 0x01})
	addRepoFiles(t, dir, "blob.bin")

	code, stdout, stderr := runCLI(t, "--format", "sarif", dir)
	if code != exitFindings {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}

	var document map[string]any
	if err := json.Unmarshal([]byte(stdout), &document); err != nil {
		t.Fatalf("invalid SARIF JSON: %v", err)
	}

	if document["version"] != "2.1.0" {
		t.Fatalf("SARIF version = %v", document["version"])
	}

	if !strings.Contains(stdout, "BC001") {
		t.Fatal("SARIF output missing rule ID")
	}
}

func TestRunBaselineAcceptsKnownFindings(t *testing.T) {
	dir := newGitRepo(t)
	writeRepoFile(t, dir, "blob.bin", []byte{0x00, 0x01})
	addRepoFiles(t, dir, "blob.bin")

	baselinePath := filepath.Join(t.TempDir(), "baseline.json")

	code, _, stderr := runCLI(t, "--save-baseline", baselinePath, dir)
	if code != exitFindings {
		t.Fatalf("save run exit = %d, stderr = %s", code, stderr)
	}

	code, stdout, stderr := runCLI(t, "--baseline", baselinePath, dir)
	if code != exitClean {
		t.Fatalf("baseline run exit = %d, want 0 (stderr: %s)", code, stderr)
	}

	if !strings.Contains(stdout, "No actionable findings") {
		t.Fatalf("stdout = %s", stdout)
	}

	code, stdout, _ = runCLI(t, "--baseline", baselinePath, "--show-baselined", dir)
	if code != exitClean {
		t.Fatalf("show-baselined exit = %d", code)
	}

	if !strings.Contains(stdout, "BASELINED") {
		t.Fatalf("stdout missing baselined section: %s", stdout)
	}
}

func TestRunSuppressedFindingsAreHiddenByDefault(t *testing.T) {
	dir := newGitRepo(t)
	writeRepoFile(t, dir, ".binary-checker.json", []byte(
		`{"ignore":[{"path":"blob.bin","reason":"fixture"}]}`,
	))
	writeRepoFile(t, dir, "blob.bin", []byte{0x00, 0x01})
	addRepoFiles(t, dir, ".binary-checker.json", "blob.bin")

	code, stdout, stderr := runCLI(t, dir)
	if code != exitClean {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}

	if strings.Contains(stdout, "BC001") {
		t.Fatalf("suppressed finding leaked into default output: %s", stdout)
	}

	code, stdout, _ = runCLI(t, "--show-suppressed", dir)
	if code != exitClean {
		t.Fatalf("exit = %d", code)
	}

	if !strings.Contains(stdout, "SUPPRESSED") || !strings.Contains(stdout, "BC001") {
		t.Fatalf("stdout missing suppressed section: %s", stdout)
	}
}

func TestRunListRules(t *testing.T) {
	code, stdout, stderr := runCLI(t, "--list")
	if code != exitClean {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}

	for _, want := range []string{"BC001", "BC002", "BC003", "DEFAULT"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q: %s", want, stdout)
		}
	}
}

func TestRunRejectsUnknownRule(t *testing.T) {
	code, _, stderr := runCLI(t, "--enable", "BC999", ".")
	if code != exitOperational {
		t.Fatalf("exit = %d, want %d", code, exitOperational)
	}

	if !strings.Contains(stderr, "unknown rule") {
		t.Fatalf("stderr = %s", stderr)
	}
}

func TestRunNotGitRepository(t *testing.T) {
	dir := t.TempDir()

	code, _, stderr := runCLI(t, dir)
	if code != exitOperational {
		t.Fatalf("exit = %d, want %d", code, exitOperational)
	}

	if !strings.Contains(stderr, "not a git repository") {
		t.Fatalf("stderr = %s", stderr)
	}
}
