package binarycheck

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/larsartmann/go-finding"
	linter "github.com/larsartmann/go-linter-sdk"
)

// ToolName is the stable tool identifier used in findings, configuration, and
// BuildFlow registration.
const ToolName = "binary-check"

// RuleID is the stable identifier of a rule. Ignore entries, baselines, and
// suppressions key on it, so the values never change.
type RuleID string

const (
	// RuleTrackedBinary reports a tracked file whose content is binary.
	RuleTrackedBinary RuleID = "BC001"
	// RuleTrackedPattern reports a tracked file matching a binary artifact
	// pattern whose content reads as text.
	RuleTrackedPattern RuleID = "BC002"
	// RuleTrackedIgnored reports a tracked file that also matches .gitignore.
	RuleTrackedIgnored RuleID = "BC003"
)

// Finding tags. Shared vocabulary so every rule that concerns git-tracked
// artifacts carries the same labels.
const (
	tagGit               finding.Tag = "git"
	tagBinary            finding.Tag = "binary"
	tagBuildArtifact     finding.Tag = "build-artifact"
	tagBestPractice      finding.Tag = "best-practice"
	tagRepositoryHygiene finding.Tag = "repository-hygiene"
	tagRuleConfiguration finding.Tag = "configuration"
)

// Metadata keys on a finding. Namespaced with the tool name, as go-finding
// asks, so they cannot collide with another tool's keys.
const (
	metadataSize     = "binary-check.size"
	metadataEvidence = "binary-check.evidence"
	metadataPattern  = "binary-check.pattern"
)

// ruleSpec declares one rule: its identity in the registry and how a finding
// for it reads. A rule is data, so adding one adds an entry, not a branch.
type ruleSpec struct {
	id          RuleID
	name        string
	description string
	category    linter.Category
	severity    finding.Severity
	confidence  finding.Confidence
	tags        []finding.Tag
	optIn       bool
	// confidenceOf overrides confidence per finding; nil keeps confidence.
	confidenceOf func(fileInspection) finding.Confidence
	message      func(fileInspection) string
	suggestion   func(path string) string
	// metadataOf adds rule-specific facts to each finding; nil adds none.
	metadataOf func(fileInspection) map[string]string
}

var ( //nolint:gochecknoglobals // the rule registry is static by definition
	ruleTrackedBinary = &ruleSpec{
		id:          RuleTrackedBinary,
		name:        "tracked binary file",
		description: "A git-tracked file contains binary data (NUL byte or low printable-text ratio). Build artifacts must not be committed.",
		category:    linter.CategoryBestPractice,
		severity:    finding.SeverityError,
		confidence:  finding.ConfidenceFull,
		tags:        []finding.Tag{tagGit, tagBinary, tagBuildArtifact, tagBestPractice},
		confidenceOf: func(inspection fileInspection) finding.Confidence {
			return verdictConfidence(inspection.verdict)
		},
		metadataOf: inspectionMetadata,
		message: func(inspection fileInspection) string {
			message := fmt.Sprintf(
				"tracked binary file %q (%d bytes; detected by %s)",
				inspection.file.Path,
				inspection.size,
				inspection.verdict.Evidence,
			)
			if inspection.pattern != "" {
				message += fmt.Sprintf(" and matching %q", inspection.pattern)
			}

			return message
		},
		suggestion: func(path string) string {
			return fmt.Sprintf(
				"remove %s from git (`git rm --cached %s`) and ignore it, or store it in Git LFS",
				path,
				path,
			)
		},
	}

	ruleTrackedPattern = &ruleSpec{
		id:          RuleTrackedPattern,
		name:        "tracked binary artifact with text content",
		description: "A git-tracked file matches a binary artifact pattern but its content reads as text, which usually means an empty or placeholder artifact was committed.",
		category:    linter.CategoryBestPractice,
		severity:    finding.SeverityWarning,
		confidence:  finding.ConfidenceMedium,
		tags:        []finding.Tag{tagGit, tagBinary, tagBuildArtifact, tagBestPractice},
		metadataOf:  inspectionMetadata,
		message: func(inspection fileInspection) string {
			return fmt.Sprintf(
				"tracked file %q matches binary artifact pattern %q but its content reads as text",
				inspection.file.Path,
				inspection.pattern,
			)
		},
		suggestion: func(path string) string {
			return fmt.Sprintf(
				"verify this is not a build artifact committed by mistake; remove it with `git rm --cached %s` or add a .binary-checker.json ignore entry with a reason",
				path,
			)
		},
	}

	ruleTrackedIgnored = &ruleSpec{
		id:          RuleTrackedIgnored,
		name:        "tracked file matches .gitignore",
		description: "A file is tracked even though .gitignore matches it. Git ignores apply only to untracked files, so the ignore rule is silently inert until the file is untracked.",
		category:    linter.CategoryConfiguration,
		severity:    finding.SeverityWarning,
		confidence:  finding.ConfidenceFull,
		tags:        []finding.Tag{tagGit, tagRepositoryHygiene, tagRuleConfiguration},
		optIn:       true,
		message: func(inspection fileInspection) string {
			return fmt.Sprintf(
				"tracked file %q matches .gitignore; ignore rules do not apply to tracked files",
				inspection.file.Path,
			)
		},
		suggestion: func(path string) string {
			return fmt.Sprintf("untrack it with `git rm --cached %s`", path)
		},
	}

	// ruleTable lists every rule in registry order.
	ruleTable = []*ruleSpec{ruleTrackedBinary, ruleTrackedPattern, ruleTrackedIgnored}
)

// inspectionMetadata records what the content inspection measured.
func inspectionMetadata(inspection fileInspection) map[string]string {
	return map[string]string{
		metadataSize:     strconv.FormatInt(inspection.size, 10),
		metadataEvidence: string(inspection.verdict.Evidence),
		metadataPattern:  inspection.pattern,
	}
}

// AllRuleIDs returns every rule ID in registry order.
func AllRuleIDs() []RuleID {
	ids := make([]RuleID, 0, len(ruleTable))
	for _, rule := range ruleTable {
		ids = append(ids, rule.id)
	}

	return ids
}

// JoinRuleIDs lists rule IDs comma-separated, for messages that name them.
func JoinRuleIDs(ids []RuleID) string {
	parts := make([]string, len(ids))
	for index, id := range ids {
		parts[index] = string(id)
	}

	return strings.Join(parts, ", ")
}

// Rules builds the rule set over one shared Checker. Every rule is included,
// opt-in ones too: selecting which rules run is the caller's decision.
func Rules(checker *Checker) []linter.Rule {
	rules := make([]linter.Rule, 0, len(ruleTable))

	for _, spec := range ruleTable {
		rule := checkerRule{
			checker: checker,
			Meta: linter.RuleMeta{
				ID:          string(spec.id),
				Name:        spec.name,
				Description: spec.description,
				Cat:         spec.category,
				Sev:         spec.severity,
				ToolName:    finding.ToolName(ToolName),
			},
			Run: ruleRunner(checker, spec.id),
		}

		if spec.optIn {
			rules = append(rules, optInRule{rule})
		} else {
			rules = append(rules, rule)
		}
	}

	return rules
}

// NewRegistry builds a registry containing the enabled-by-default rules.
// Opt-in rules (BC003) must be selected explicitly by the caller.
func NewRegistry(checker *Checker) *linter.Registry {
	if checker == nil {
		checker = NewChecker()
	}

	registry := linter.NewRegistry(linter.WithToolName(ToolName))

	for _, rule := range Rules(checker) {
		if rule.IsEnabledByDefault() {
			registry.Register(rule)
		}
	}

	return registry
}

// checkerRule is one rule of the registry. It keeps a handle on the shared
// Checker so every rule reads the same memoized scan.
type checkerRule struct {
	linter.RuleFunc

	checker *Checker
}

// Coverage reports how many tracked files the shared scan inspected.
func (r checkerRule) Coverage(ctx context.Context, dir string) (int, []string) {
	result, err := r.checker.Scan(ctx, dir)
	if err != nil {
		return 0, nil
	}

	return result.FilesScanned, nil
}

// optInRule turns its rule off by default. It wraps checkerRule rather than
// using linter.OptIn, which accepts a bare RuleFunc and would drop the
// Coverage method hosts rely on for "clean tree" versus "scanned nothing".
type optInRule struct {
	checkerRule
}

func (optInRule) IsEnabledByDefault() bool { return false }

// ruleRunner narrows the shared scan to one rule's findings.
func ruleRunner(checker *Checker, ruleID RuleID) func(context.Context, string) ([]finding.Finding, error) {
	return func(ctx context.Context, dir string) ([]finding.Finding, error) {
		result, err := checker.Scan(ctx, dir)
		if err != nil {
			return nil, err
		}

		return finding.Filter(result.Findings, finding.ByRule(finding.RuleName(ruleID))), nil
	}
}
