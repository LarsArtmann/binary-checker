package binarycheck

import (
	"context"
	"sync"

	"github.com/larsartmann/go-finding"
)

const maxScanWorkers = 8

// ScanResult is one repository scan. Findings are sorted by position, then
// rule, so every output format is deterministic.
type ScanResult struct {
	// Root is the git work-tree root that was scanned.
	Root string
	// FilesScanned counts tracked files the scan examined, exempt ones
	// included.
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
	// Findings contains one finding per offending file per rule, so a file
	// can carry several.
	Findings []finding.Finding
}

// Checker scans repositories. A checker memoizes the result per repository,
// so the rules of one registry run share a single git walk and content pass.
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

// Scan inspects every tracked file under dir's git work tree. The result is
// memoized per work tree: scanning a subdirectory of a repository already
// scanned returns that repository's result.
func (c *Checker) Scan(ctx context.Context, dir string) (*ScanResult, error) {
	if dir == "" {
		dir = "."
	}

	git, err := newGitClient(ctx, dir)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	cached, ok := c.cache[git.root]
	c.mu.Unlock()

	if ok {
		return cached, nil
	}

	result, err := c.scan(ctx, git)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.cache[git.root] = result
	c.mu.Unlock()

	return result, nil
}

func (c *Checker) scan(ctx context.Context, git *gitClient) (*ScanResult, error) {
	config, err := LoadConfig(c.configPath, git.root)
	if err != nil {
		return nil, err
	}

	tracked, contentLess, err := git.tracked(ctx)
	if err != nil {
		return nil, err
	}

	lfs, err := git.lfsPaths(ctx, trackedPaths(tracked))
	if err != nil {
		return nil, err
	}

	trackedIgnored, err := git.trackedIgnored(ctx)
	if err != nil {
		return nil, err
	}

	inspections, err := inspectAll(ctx, git.root, tracked, config, lfs)
	if err != nil {
		return nil, err
	}

	result := &ScanResult{Root: git.root, FilesSkipped: contentLess}

	for _, inspection := range inspections {
		if err := result.collect(config, inspection); err != nil {
			return nil, err
		}
	}

	for _, path := range trackedIgnored {
		if err := result.collectTrackedIgnored(config, path); err != nil {
			return nil, err
		}
	}

	SortFindings(result.Findings)

	return result, nil
}

func trackedPaths(files []trackedFile) []string {
	paths := make([]string, len(files))
	for index, file := range files {
		paths[index] = file.Path
	}

	return paths
}

// collect records one inspection: why a clean file was not flagged, or the
// finding an offending file produced.
func (r *ScanResult) collect(config Config, inspection fileInspection) error {
	rule := ruleForInspection(inspection)
	if rule == nil {
		r.accountUnflagged(inspection)

		return nil
	}

	r.FilesScanned++

	reason, suppressed := config.SuppressionReason(inspection.file.Path, rule.id)
	if suppressed {
		r.Suppressed++
	}

	item, err := buildFinding(rule, inspection, reason)
	if err != nil {
		return err
	}

	r.Findings = append(r.Findings, item)

	return nil
}

// accountUnflagged records why an inspection produced no finding.
func (r *ScanResult) accountUnflagged(inspection fileInspection) {
	switch inspection.disposition {
	case dispositionInspected:
		r.FilesScanned++
	case dispositionExempt:
		r.FilesScanned++
		r.ExemptFiles++
	case dispositionLFS:
		r.LFSFiles++
	case dispositionSkipped:
		r.FilesSkipped++
	}
}

// collectTrackedIgnored records the BC003 finding for one tracked-but-ignored
// path.
//
// Every such path is reported, whether or not another rule already flagged the
// file: BC003 answers a different question ("should this file be ignored?"),
// and skipping flagged files would let --disable BC002 hide a BC003 finding
// the user explicitly asked for.
func (r *ScanResult) collectTrackedIgnored(config Config, path string) error {
	reason, suppressed := config.SuppressionReason(path, RuleTrackedIgnored)
	if suppressed {
		r.Suppressed++
	}

	item, err := buildFinding(ruleTrackedIgnored, fileInspection{file: trackedFile{Path: path}}, reason)
	if err != nil {
		return err
	}

	r.Findings = append(r.Findings, item)

	return nil
}
