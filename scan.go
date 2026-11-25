package binarycheck

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"sync"

	errorfamily "github.com/larsartmann/go-error-family"
	"github.com/larsartmann/go-finding"
)

const (
	maxScanWorkers = 8
	codeScan       = "binary_checker.scan"
	codeRule       = "binary_checker.rule"
)

// ScanResult is one repository scan. Findings are sorted by path, line, then
// rule so every output format is deterministic.
type ScanResult struct {
	// Root is the git work-tree root that was scanned.
	Root string
	// FilesScanned counts tracked files whose content was inspected.
	FilesScanned int
	// FilesSkipped counts index entries that could not be inspected (missing
	// from the working tree, unreadable, symlink, submodule).
	FilesSkipped int
	// LFSFiles counts files skipped because their content lives in Git LFS.
	LFSFiles int
	// ExemptFiles counts fonts and small images allowed by policy.
	ExemptFiles int
	// Suppressed counts findings silenced by .binary-checker.json ignore
	// entries. They remain in Findings with Suppression attached.
	Suppressed int
	// Findings contains one finding per offending file per rule.
	Findings []finding.Finding
}

// Checker scans repositories. A checker memoizes scans per directory so the
// rules of one registry run share a single git walk and content pass.
type Checker struct {
	configPath string

	mu    sync.Mutex
	cache map[string]*ScanResult
}

// CheckerOption configures a Checker.
type CheckerOption func(*Checker)

// WithConfigPath pins the config file path. An empty value (the default)
// auto-discovers .binary-checker.json at the git root.
func WithConfigPath(path string) CheckerOption {
	return func(c *Checker) { c.configPath = path }
}

// NewChecker creates a Checker.
func NewChecker(opts ...CheckerOption) *Checker {
	checker := &Checker{cache: make(map[string]*ScanResult)}

	for _, opt := range opts {
		opt(checker)
	}

	return checker
}

// Scan inspects every tracked file under dir's git work tree.
func (c *Checker) Scan(ctx context.Context, dir string) (*ScanResult, error) {
	if dir == "" {
		dir = "."
	}

	key, err := filepath.Abs(dir)
	if err != nil {
		return nil, errorfamily.WrapInfrastructuref(err, codeScan, "resolve scan directory %q", dir)
	}

	c.mu.Lock()
	cached, ok := c.cache[key]
	c.mu.Unlock()

	if ok {
		return cached, nil
	}

	result, err := c.scan(ctx, dir)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.cache[key] = result
	c.mu.Unlock()

	return result, nil
}

func (c *Checker) scan(ctx context.Context, dir string) (*ScanResult, error) {
	git, err := newGitClient(ctx, dir)
	if err != nil {
		return nil, err
	}

	config, err := LoadConfig(c.configPath, git.root)
	if err != nil {
		return nil, err
	}

	tracked, indexSkipped, err := git.tracked(ctx)
	if err != nil {
		return nil, err
	}

	trackedIgnored, err := git.trackedIgnored(ctx)
	if err != nil {
		return nil, err
	}

	paths := make([]string, len(tracked))
	for i, file := range tracked {
		paths[i] = file.Path
	}

	lfs := git.lfsPaths(ctx, paths)

	inspections := inspectAll(ctx, git.root, tracked, config, lfs)

	result := &ScanResult{Root: git.root, FilesSkipped: indexSkipped}

	flagged := make(map[string]bool)

	for _, inspection := range inspections {
		didFlag, inspectErr := c.collectInspection(config, result, inspection)
		if inspectErr != nil {
			return nil, inspectErr
		}

		if didFlag {
			flagged[inspection.file.Path] = true
		}
	}

	for _, path := range trackedIgnored {
		if flagged[path] {
			continue
		}

		reason, suppressed := config.SuppressionReason(path, RuleTrackedIgnored)
		if suppressed {
			result.Suppressed++
		}

		item, buildErr := buildIgnoredFinding(path, reason)
		if buildErr != nil {
			return nil, buildErr
		}

		result.Findings = append(result.Findings, item)
		flagged[path] = true
	}

	SortFindings(result.Findings)

	return result, nil
}

// collectInspection records one inspection in result and reports whether the
// file produced a finding, so the tracked-ignored pass does not double count it.
func (c *Checker) collectInspection(
	config Config,
	result *ScanResult,
	inspection fileInspection,
) (bool, error) {
	ruleID, matched := c.ruleForInspection(inspection)
	if !matched {
		result.accountUnflagged(inspection)

		return false, nil
	}

	result.FilesScanned++

	reason, suppressed := config.SuppressionReason(inspection.file.Path, ruleID)
	if suppressed {
		result.Suppressed++
	}

	item, buildErr := buildFinding(ruleID, inspection, reason)
	if buildErr != nil {
		return false, errorfamily.WrapCorruptionf(
			buildErr, codeRule, "collect %s finding for %q", ruleID, inspection.file.Path,
		)
	}

	result.Findings = append(result.Findings, item)

	return true, nil
}

// accountUnflagged records why an inspection produced no finding.
func (r *ScanResult) accountUnflagged(inspection fileInspection) {
	switch {
	case inspection.skip:
		r.FilesSkipped++
	case inspection.lfs:
		r.LFSFiles++
	default:
		r.FilesScanned++
	}

	if inspection.exempt {
		r.ExemptFiles++
	}
}

// ruleForInspection decides which (if any) rule an inspected file violates.
func (c *Checker) ruleForInspection(inspection fileInspection) (string, bool) {
	switch {
	case inspection.skip || inspection.lfs || inspection.exempt:
		return "", false
	case inspection.verdict.Binary:
		return RuleTrackedBinary, true
	case inspection.pattern != "":
		return RuleTrackedPattern, true
	default:
		return "", false
	}
}

type fileInspection struct {
	file    trackedFile
	size    int64
	verdict ContentVerdict
	pattern string
	lfs     bool
	exempt  bool
	skip    bool
}

func inspectAll(
	ctx context.Context,
	root string,
	files []trackedFile,
	config Config,
	lfs map[string]bool,
) []fileInspection {
	workers := max(min(runtime.NumCPU(), maxScanWorkers), 1)

	inspections := make([]fileInspection, len(files))
	jobs := make(chan int)

	var waitGroup sync.WaitGroup
	for range workers {
		waitGroup.Go(func() {
			for index := range jobs {
				inspections[index] = inspectFile(ctx, root, files[index], config, lfs)
			}
		})
	}

	for index := range files {
		jobs <- index
	}

	close(jobs)
	waitGroup.Wait()

	return inspections
}

func inspectFile(
	ctx context.Context,
	root string,
	file trackedFile,
	config Config,
	lfs map[string]bool,
) fileInspection {
	inspection := fileInspection{file: file}

	if ctx.Err() != nil {
		inspection.skip = true

		return inspection
	}

	if lfs[file.Path] {
		inspection.lfs = true

		return inspection
	}

	absolute := filepath.Join(root, filepath.FromSlash(file.Path))

	info, err := os.Stat(absolute)
	if err != nil || info.IsDir() {
		inspection.skip = true

		return inspection
	}

	inspection.size = info.Size()
	inspection.pattern = MatchBinaryArtifact(file.Path)

	if inspection.size == 0 {
		return inspection
	}

	sample, err := ReadSample(absolute)
	if err != nil {
		inspection.skip = true

		return inspection
	}

	inspection.verdict = ClassifyContent(sample)
	inspection.exempt = isExemptAsset(file.Path, inspection.size, config)

	return inspection
}

func isExemptAsset(relPath string, size int64, config Config) bool {
	if IsFont(relPath) && config.AllowFontsEnabled() && size <= config.FontCeiling() {
		return true
	}

	return IsSmallImage(relPath) && config.AllowSmallImagesEnabled() && size <= config.SmallImageCeiling()
}

// SortFindings orders findings by path, line, then rule so every output format
// is deterministic regardless of scan order.
func SortFindings(findings []finding.Finding) {
	sort.Slice(findings, func(i, j int) bool {
		left, right := findings[i], findings[j]

		if left.Position.File != right.Position.File {
			return left.Position.File < right.Position.File
		}

		if left.Position.Line != right.Position.Line {
			return left.Position.Line < right.Position.Line
		}

		return left.Rule < right.Rule
	})
}

func buildFinding(ruleID string, inspection fileInspection, suppressionReason string) (finding.Finding, error) {
	rule := ruleByID(ruleID)
	if rule == nil {
		return finding.Finding{}, errorfamily.Newf(errorfamily.Corruption,
			codeRule, "unknown rule %q while building finding for %q (suppression reason=%q)",
			ruleID, inspection.file.Path, suppressionReason,
		)
	}

	confidence := rule.confidence
	if ruleID == RuleTrackedBinary && inspection.verdict.Evidence != "nul-byte" {
		confidence = finding.ConfidenceHigh
	}

	builder := finding.NewBuilder(
		finding.RuleName(ruleID),
		finding.ToolName(ToolName),
		rule.message(inspection),
		rule.severity,
		finding.FilePos(finding.FilePath(inspection.file.Path)),
	).
		WithCategory(finding.Category(rule.category)).
		WithConfidence(confidence).
		WithFixStrategy(finding.FixStrategyNone).
		WithSuggestion(rule.suggestion(inspection.file.Path)).
		WithTags(rule.tags...).
		WithMetadata(map[string]string{
			"binary-check.size":     strconv.FormatInt(inspection.size, 10),
			"binary-check.evidence": inspection.verdict.Evidence,
			"binary-check.pattern":  inspection.pattern,
		})

	if suppressionReason != "" {
		builder = builder.WithSuppression(finding.Suppression{
			Kind:   finding.SuppressionInConfig,
			Rule:   finding.RuleName(ruleID),
			Reason: suppressionReason,
		})
	}

	item, err := builder.Build()
	if err != nil {
		return finding.Finding{}, errorfamily.WrapCorruptionf(err, codeRule, "build finding")
	}

	return item, nil
}

func buildIgnoredFinding(path, suppressionReason string) (finding.Finding, error) {
	rule := ruleByID(RuleTrackedIgnored)
	if rule == nil {
		return finding.Finding{}, errorfamily.Newf(errorfamily.Corruption,
			codeRule, "unknown rule %q while building %q (suppression reason=%q)",
			RuleTrackedIgnored, path, suppressionReason,
		)
	}

	builder := finding.NewBuilder(
		finding.RuleName(RuleTrackedIgnored),
		finding.ToolName(ToolName),
		fmt.Sprintf("tracked file %q matches .gitignore; ignore rules do not apply to tracked files", path),
		rule.severity,
		finding.FilePos(finding.FilePath(path)),
	).
		WithCategory(finding.Category(rule.category)).
		WithConfidence(rule.confidence).
		WithFixStrategy(finding.FixStrategyNone).
		WithSuggestion(fmt.Sprintf("untrack it with `git rm --cached %s`", path)).
		WithTags(rule.tags...)

	if suppressionReason != "" {
		builder = builder.WithSuppression(finding.Suppression{
			Kind:   finding.SuppressionInConfig,
			Rule:   finding.RuleName(RuleTrackedIgnored),
			Reason: suppressionReason,
		})
	}

	item, err := builder.Build()
	if err != nil {
		return finding.Finding{}, errorfamily.WrapCorruptionf(
			err, codeRule, "build %s finding for %q", RuleTrackedIgnored, path,
		)
	}

	return item, nil
}
