package binarycheck

import (
	"context"
	"errors"
	"testing"

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
		rule  string
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
