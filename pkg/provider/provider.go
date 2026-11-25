// Package provider self-registers binary-check as a BuildFlow tool provider
// through the go-finding toolsdk.
//
// BuildFlow blank-imports this package (tools/providers/sdk_imports.go) and
// discovers the spec via toolsdk.All(): no BuildFlow-side glue, no dependency
// on BuildFlow internals. The detector drops findings suppressed by
// .binary-checker.json so BuildFlow's severity gate counts only actionable
// findings; the standalone CLI still shows suppressed findings on request.
package provider

import (
	"context"
	"os/exec"
	"sort"

	"github.com/larsartmann/go-finding"
	toolsdk "github.com/larsartmann/go-finding/toolsdk"
	errorfamily "github.com/larsartmann/go-error-family"
	linter "github.com/larsartmann/go-linter-sdk"

	binarycheck "github.com/larsartmann/binary-checker"
)

// Provider is the BuildFlow registration. A package-level var so the
// registration runs at init time.
var Provider = toolsdk.Register(toolsdk.Spec{
	Name:    binarycheck.ToolName,
	Trigger: toolsdk.AnyLanguage(),
	Description: "Detects git-tracked binary files that should not be committed " +
		"(NUL-byte/text-ratio content analysis plus binary artifact patterns; " +
		"Git LFS pointers, symlinks, submodules, and size-bounded font/image assets are exempt). " +
		"Detector-only: it reports and suggests index cleanup, never rewrites git.",
	Detect:      newDetector(),
	HealthCheck: gitHealthCheck,
})

func gitHealthCheck(ctx context.Context) error {
	if _, err := exec.LookPath("git"); err != nil {
		return errorfamily.WrapInfrastructuref(
			err, "binary_checker.git", "binary-check requires git on PATH",
		)
	}

	return nil
}

func newDetector() finding.Detector {
	return &suppressionFilteringDetector{
		inner: linter.DetectorFromRegistry(binarycheck.NewRegistry(nil), binarycheck.ToolName),
	}
}

// suppressionFilteringDetector removes findings silenced by config, keeping
// the V2 coverage contract intact.
type suppressionFilteringDetector struct {
	inner finding.Detector
}

func (d *suppressionFilteringDetector) Name() string { return d.inner.Name() }

func (d *suppressionFilteringDetector) Detect(ctx context.Context) ([]finding.Finding, error) {
	findings, err := d.inner.Detect(ctx)
	if err != nil {
		return nil, err
	}

	return activeFindings(findings), nil
}

func (d *suppressionFilteringDetector) DetectV2(ctx context.Context) (finding.DetectResult, error) {
	result, err := finding.RunDetect(ctx, d.inner)
	if err != nil {
		return result, err
	}

	result.Findings = activeFindings(result.Findings)

	return result, nil
}

func activeFindings(findings []finding.Finding) []finding.Finding {
	active := make([]finding.Finding, 0, len(findings))

	for _, item := range findings {
		if item.IsSuppressed() {
			continue
		}

		active = append(active, item)
	}

	sort.Slice(active, func(i, j int) bool {
		if active[i].Position.File != active[j].Position.File {
			return active[i].Position.File < active[j].Position.File
		}

		return active[i].Rule < active[j].Rule
	})

	return active
}
