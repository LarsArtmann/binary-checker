package provider

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/larsartmann/go-finding"
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

func TestProviderSpec(t *testing.T) {
	t.Parallel()

	if Provider.Name != "binary-check" {
		t.Fatalf("Name = %q", Provider.Name)
	}

	if Provider.Description == "" {
		t.Fatal("Description is required by toolsdk")
	}

	if Provider.Trigger.Language != "" || len(Provider.Trigger.Files) != 0 {
		t.Fatalf("expected a language-agnostic always-on trigger, got %+v", Provider.Trigger)
	}

	if Provider.Detect == nil || Provider.HealthCheck == nil {
		t.Fatal("Detect and HealthCheck must be registered")
	}
}

func TestDetectReportsTrackedBinary(t *testing.T) {
	dir := newGitRepo(t)
	writeRepoFile(t, dir, "blob.bin", []byte{0x00, 0x01, 0x02})
	addRepoFiles(t, dir, "blob.bin")

	ctx := finding.WithWorkingDir(context.Background(), dir)

	findings, err := Provider.Detect.Detect(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(findings))
	}

	if string(findings[0].Rule) != "BC001" {
		t.Fatalf("rule = %q", findings[0].Rule)
	}
}

func TestDetectDropsConfigSuppressedFindings(t *testing.T) {
	dir := newGitRepo(t)
	writeRepoFile(t, dir, ".binary-checker.json", []byte(
		`{"ignore":[{"path":"blob.bin","reason":"fixture"}]}`,
	))
	writeRepoFile(t, dir, "blob.bin", []byte{0x00, 0x01})
	addRepoFiles(t, dir, ".binary-checker.json", "blob.bin")

	ctx := finding.WithWorkingDir(context.Background(), dir)

	detector, ok := Provider.Detect.(finding.DetectorV2)
	if !ok {
		t.Fatal("provider detector must implement DetectorV2 for coverage evidence")
	}

	result, err := detector.DetectV2(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Findings) != 0 {
		t.Fatalf("suppressed findings must not reach BuildFlow, got %d", len(result.Findings))
	}

	if result.FilesScanned == 0 {
		t.Fatal("expected coverage evidence (filesScanned > 0)")
	}
}

func TestDetectFailsOutsideGitRepository(t *testing.T) {
	dir := t.TempDir()

	ctx := finding.WithWorkingDir(context.Background(), dir)

	if _, err := Provider.Detect.Detect(ctx); err == nil {
		t.Fatal("expected an error outside a git repository")
	}
}
