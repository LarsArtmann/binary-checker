package provider

import (
	"context"
	"testing"

	"github.com/larsartmann/go-finding"

	"github.com/larsartmann/binary-checker/internal/gittest"
)

func TestProviderSpec(t *testing.T) {
	t.Parallel()

	if Provider.Name != "binary-check" {
		t.Fatalf("Name = %q", Provider.Name)
	}

	if Provider.Description == "" {
		t.Fatal("Description is required by toolsdk")
	}

	if Provider.Trigger.Language != "" {
		t.Fatalf("expected a language-agnostic trigger, got %+v", Provider.Trigger)
	}

	// Consumer contract: an empty trigger file set leaves BuildFlow's result
	// cache blind to every file this detector reads (the key falls back to the
	// trigger patterns when no FileContract is declared), so a cached "clean"
	// verdict would survive any edit to the tree being scanned.
	if len(Provider.Trigger.Files) == 0 {
		t.Fatal("trigger must declare file patterns; an empty set makes the consumer's cache key blind")
	}

	if Provider.Detect == nil || Provider.HealthCheck == nil {
		t.Fatal("Detect and HealthCheck must be registered")
	}
}

func TestDetectReportsTrackedBinary(t *testing.T) {
	t.Parallel()

	dir := gittest.NewRepo(t)
	gittest.WriteFile(t, dir, "blob.bin", []byte{0x00, 0x01, 0x02})
	gittest.AddFiles(t, dir, "blob.bin")

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
	t.Parallel()

	dir := gittest.NewRepo(t)
	gittest.WriteFile(t, dir, ".binary-checker.json", []byte(
		`{"ignore":[{"path":"blob.bin","reason":"fixture"}]}`,
	))
	gittest.WriteFile(t, dir, "blob.bin", []byte{0x00, 0x01})
	gittest.AddFiles(t, dir, ".binary-checker.json", "blob.bin")

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
	t.Parallel()

	dir := t.TempDir()

	ctx := finding.WithWorkingDir(context.Background(), dir)

	if _, err := Provider.Detect.Detect(ctx); err == nil {
		t.Fatal("expected an error outside a git repository")
	}
}
