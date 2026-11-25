package binarycheck

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/larsartmann/go-finding"

	"github.com/larsartmann/binary-checker/internal/gittest"
)

func scanRepo(t *testing.T, dir string) *ScanResult {
	t.Helper()

	checker := NewChecker()

	result, err := checker.Scan(context.Background(), dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	return result
}

func findingsByRule(result *ScanResult, ruleID RuleID) []finding.Finding {
	var matched []finding.Finding

	for _, item := range result.Findings {
		if RuleID(item.Rule) == ruleID {
			matched = append(matched, item)
		}
	}

	return matched
}

func TestScanDetectsTrackedBinaryContent(t *testing.T) {
	t.Parallel()

	dir := gittest.NewRepo(t)

	gittest.WriteFile(t, dir, "README.md", []byte("hello\n"))
	gittest.WriteFile(t, dir, "blob.bin", []byte{0x00, 0x01, 0x02, 0x03})
	gittest.AddFiles(t, dir, "README.md", "blob.bin")

	result := scanRepo(t, dir)

	if result.FilesScanned != 2 {
		t.Fatalf("FilesScanned = %d, want 2", result.FilesScanned)
	}

	binaryFindings := findingsByRule(result, RuleTrackedBinary)
	if len(binaryFindings) != 1 {
		t.Fatalf("BC001 findings = %d, want 1", len(binaryFindings))
	}

	item := binaryFindings[0]
	if item.Position.File != "blob.bin" {
		t.Fatalf("position file = %q", item.Position.File)
	}

	if item.Severity != finding.SeverityError || item.Confidence != finding.ConfidenceFull {
		t.Fatalf("severity/confidence = %s/%s", item.Severity, item.Confidence)
	}

	if item.Metadata["binary-check.evidence"] != "nul-byte" {
		t.Fatalf("evidence = %q", item.Metadata["binary-check.evidence"])
	}

	if item.Suggestion == "" {
		t.Fatal("expected an actionable suggestion")
	}
}

func TestScanFlagsBinaryExtensionWithTextContent(t *testing.T) {
	t.Parallel()

	dir := gittest.NewRepo(t)

	gittest.WriteFile(t, dir, "archive.zip", []byte("this is not really a zip file"))
	gittest.AddFiles(t, dir, "archive.zip")

	result := scanRepo(t, dir)

	patternFindings := findingsByRule(result, RuleTrackedPattern)
	if len(patternFindings) != 1 {
		t.Fatalf("BC002 findings = %d, want 1", len(patternFindings))
	}

	item := patternFindings[0]
	if item.Severity != finding.SeverityWarning || item.Confidence != finding.ConfidenceMedium {
		t.Fatalf("severity/confidence = %s/%s", item.Severity, item.Confidence)
	}

	if item.Metadata["binary-check.pattern"] != "*.zip" {
		t.Fatalf("pattern = %q", item.Metadata["binary-check.pattern"])
	}
}

func TestScanExemptsSmallAssetsAndFlagsOversizedOnes(t *testing.T) {
	t.Parallel()

	dir := gittest.NewRepo(t)

	smallPNG := append([]byte{0x89, 'P', 'N', 'G', 0x1a, '\n', 0x00}, make([]byte, 16)...)
	bigPNG := append([]byte{0x89, 'P', 'N', 'G', 0x1a, '\n', 0x00}, make([]byte, DefaultSmallImageMaxBytes)...)
	smallFont := []byte{0x77, 'O', 'F', '2', 0x00, 0x01}

	bigFont := append([]byte{0x77, 'O', 'F', '2', 0x00, 0x01}, make([]byte, DefaultFontMaxBytes)...)

	gittest.WriteFile(t, dir, "assets/small.png", smallPNG)
	gittest.WriteFile(t, dir, "assets/big.png", bigPNG)
	gittest.WriteFile(t, dir, "assets/small.woff2", smallFont)
	gittest.WriteFile(t, dir, "assets/big.woff2", bigFont)
	gittest.AddFiles(t, dir, "assets")

	result := scanRepo(t, dir)

	if result.ExemptFiles != 2 {
		t.Fatalf("ExemptFiles = %d, want 2", result.ExemptFiles)
	}

	flagged := make(map[string]bool)
	for _, item := range result.Findings {
		flagged[string(item.Position.File)] = true
	}

	if flagged["assets/small.png"] || flagged["assets/small.woff2"] {
		t.Fatalf("small assets must be exempt, got findings: %v", flagged)
	}

	if !flagged["assets/big.png"] || !flagged["assets/big.woff2"] {
		t.Fatalf("oversized assets must be flagged, got findings: %v", flagged)
	}
}

func TestScanSkipsGitLFSFiles(t *testing.T) {
	t.Parallel()

	dir := gittest.NewRepo(t)

	gittest.WriteFile(t, dir, ".gitattributes", []byte("*.psd filter=lfs diff=lfs merge=lfs -text\n"))
	gittest.WriteFile(t, dir, "design/source.psd", []byte{0x38, 0x42, 0x50, 0x53, 0x00, 0x01})
	gittest.AddFiles(t, dir, ".gitattributes", "design/source.psd")

	result := scanRepo(t, dir)

	if result.LFSFiles != 1 {
		t.Fatalf("LFSFiles = %d, want 1", result.LFSFiles)
	}

	if len(findingsByRule(result, RuleTrackedBinary)) != 0 {
		t.Fatal("LFS-tracked binary must not be reported")
	}
}

func TestScanSkipsSymlinks(t *testing.T) {
	t.Parallel()

	dir := gittest.NewRepo(t)

	gittest.WriteFile(t, dir, "README.md", []byte("hello\n"))

	if err := os.Symlink("README.md", filepath.Join(dir, "alias.bin")); err != nil {
		t.Fatal(err)
	}

	gittest.AddFiles(t, dir, "README.md", "alias.bin")

	result := scanRepo(t, dir)

	if result.FilesSkipped != 1 {
		t.Fatalf("FilesSkipped = %d, want 1", result.FilesSkipped)
	}

	for _, item := range result.Findings {
		if item.Position.File == "alias.bin" {
			t.Fatal("symlink must not produce a finding")
		}
	}
}

func TestScanReportsTrackedButIgnored(t *testing.T) {
	t.Parallel()

	dir := gittest.NewRepo(t)

	gittest.WriteFile(t, dir, ".gitignore", []byte("*.log\n"))
	gittest.WriteFile(t, dir, "debug.log", []byte("debug\n"))
	gittest.AddFiles(t, dir, ".gitignore", "debug.log")

	result := scanRepo(t, dir)

	ignored := findingsByRule(result, RuleTrackedIgnored)
	if len(ignored) != 1 {
		t.Fatalf("BC003 findings = %d, want 1", len(ignored))
	}

	if ignored[0].Position.File != "debug.log" {
		t.Fatalf("position file = %q", ignored[0].Position.File)
	}
}

func TestScanAppliesConfigSuppression(t *testing.T) {
	t.Parallel()

	dir := gittest.NewRepo(t)

	gittest.WriteFile(t, dir, DefaultConfigName, []byte(
		`{"ignore":[{"path":"fixtures/**","reason":"binary fixture corpus"}]}`,
	))
	gittest.WriteFile(t, dir, "fixtures/sample.bin", []byte{0x00, 0x01})
	gittest.AddFiles(t, dir, DefaultConfigName, "fixtures/sample.bin")

	result := scanRepo(t, dir)

	if result.Suppressed != 1 {
		t.Fatalf("Suppressed = %d, want 1", result.Suppressed)
	}

	binaryFindings := findingsByRule(result, RuleTrackedBinary)
	if len(binaryFindings) != 1 {
		t.Fatalf("BC001 findings = %d, want 1", len(binaryFindings))
	}

	if !binaryFindings[0].IsSuppressed() {
		t.Fatal("expected finding to carry an active suppression")
	}

	if binaryFindings[0].Suppression.Reason != "binary fixture corpus" {
		t.Fatalf("reason = %q", binaryFindings[0].Suppression.Reason)
	}
}

func TestScanRejectsNonGitDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	_, err := NewChecker().Scan(context.Background(), dir)
	if !errors.Is(err, ErrNotGitRepository) {
		t.Fatalf("error = %v, want ErrNotGitRepository", err)
	}
}

func TestScanOrdersFindingsDeterministically(t *testing.T) {
	t.Parallel()

	dir := gittest.NewRepo(t)

	gittest.WriteFile(t, dir, "b.bin", []byte{0x00})
	gittest.WriteFile(t, dir, "a.bin", []byte{0x00})
	gittest.WriteFile(t, dir, "c.zip", []byte("not a zip"))
	gittest.AddFiles(t, dir, "b.bin", "a.bin", "c.zip")

	result := scanRepo(t, dir)

	if len(result.Findings) != 3 {
		t.Fatalf("findings = %d, want 3", len(result.Findings))
	}

	want := []string{"a.bin", "b.bin", "c.zip"}
	for index, item := range result.Findings {
		if string(item.Position.File) != want[index] {
			t.Fatalf("finding[%d] file = %q, want %q", index, item.Position.File, want[index])
		}
	}
}

func TestCheckerMemoizesScans(t *testing.T) {
	t.Parallel()

	dir := gittest.NewRepo(t)
	gittest.WriteFile(t, dir, "blob.bin", []byte{0x00})
	gittest.AddFiles(t, dir, "blob.bin")

	checker := NewChecker()

	first, err := checker.Scan(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}

	second, err := checker.Scan(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}

	if first != second {
		t.Fatal("expected the checker to reuse the scanned result")
	}
}
