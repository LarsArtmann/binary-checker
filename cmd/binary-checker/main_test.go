package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/larsartmann/binary-checker/internal/gittest"
)

func runCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()

	var stdout, stderr bytes.Buffer

	code := run(args, &stdout, &stderr)

	return code, stdout.String(), stderr.String()
}

func TestRunReportsActionableFindings(t *testing.T) {
	t.Parallel()

	dir := gittest.NewRepo(t)
	gittest.WriteFile(t, dir, "blob.bin", []byte{0x00, 0x01})
	gittest.AddFiles(t, dir, "blob.bin")

	code, stdout, stderr := runCLI(t, dir)
	if code != exitFindings {
		t.Fatalf("exit = %d, want %d (stderr: %s)", code, exitFindings, stderr)
	}

	if !strings.Contains(stdout, "BC001") || !strings.Contains(stdout, "blob.bin") {
		t.Fatalf("stdout missing finding: %s", stdout)
	}
}

func TestRunTriageExitCodeForLowConfidenceFindings(t *testing.T) {
	t.Parallel()

	dir := gittest.NewRepo(t)
	gittest.WriteFile(t, dir, "archive.zip", []byte("not a zip"))
	gittest.AddFiles(t, dir, "archive.zip")

	code, stdout, _ := runCLI(t, dir)
	if code != exitTriage {
		t.Fatalf("exit = %d, want %d", code, exitTriage)
	}

	if !strings.Contains(stdout, "TRIAGE") || !strings.Contains(stdout, "BC002") {
		t.Fatalf("stdout missing triage finding: %s", stdout)
	}
}

func TestRunJSONFormat(t *testing.T) {
	t.Parallel()

	dir := gittest.NewRepo(t)
	gittest.WriteFile(t, dir, "blob.bin", []byte{0x00, 0x01})
	gittest.AddFiles(t, dir, "blob.bin")

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
	t.Parallel()

	dir := gittest.NewRepo(t)
	gittest.WriteFile(t, dir, "blob.bin", []byte{0x00, 0x01})
	gittest.AddFiles(t, dir, "blob.bin")

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
	t.Parallel()

	dir := gittest.NewRepo(t)
	gittest.WriteFile(t, dir, "blob.bin", []byte{0x00, 0x01})
	gittest.AddFiles(t, dir, "blob.bin")

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
	t.Parallel()

	dir := gittest.NewRepo(t)
	gittest.WriteFile(t, dir, ".binary-checker.json", []byte(
		`{"ignore":[{"path":"blob.bin","reason":"fixture"}]}`,
	))
	gittest.WriteFile(t, dir, "blob.bin", []byte{0x00, 0x01})
	gittest.AddFiles(t, dir, ".binary-checker.json", "blob.bin")

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
	t.Parallel()

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
	t.Parallel()

	code, _, stderr := runCLI(t, "--enable", "BC999", ".")
	if code != exitUsage {
		t.Fatalf("exit = %d, want %d", code, exitUsage)
	}

	if !strings.Contains(stderr, "unknown rule") {
		t.Fatalf("stderr = %s", stderr)
	}
}

func TestRunNotGitRepository(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	code, _, stderr := runCLI(t, dir)
	if code != exitUsage {
		t.Fatalf("exit = %d, want %d", code, exitUsage)
	}

	if !strings.Contains(stderr, "not a git repository") {
		t.Fatalf("stderr = %s", stderr)
	}

	// The scan fails once; it must be reported once, not once per rule.
	if reported := strings.Count(stderr, "Error:"); reported != 1 {
		t.Fatalf("stderr reports the failure %d times: %s", reported, stderr)
	}
}
