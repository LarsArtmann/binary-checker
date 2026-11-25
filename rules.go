package binarycheck

import (
	"context"
	"fmt"

	"github.com/larsartmann/go-finding"
	linter "github.com/larsartmann/go-linter-sdk"
)

// ToolName is the stable tool identifier used in findings, configuration, and
// BuildFlow registration.
const ToolName = "binary-check"

// Stable rule IDs. They never change: suppressions, ignore entries, and
// baselines key on them.
const (
	// RuleTrackedBinary reports a tracked file whose content is binary.
	RuleTrackedBinary = "BC001"
	// RuleTrackedPattern reports a tracked file matching a binary artifact
	// pattern whose content reads as text.
	RuleTrackedPattern = "BC002"
	// RuleTrackedIgnored reports a tracked file that also matches .gitignore.
	RuleTrackedIgnored = "BC003"
)

// Finding tags. Shared vocabulary so every rule that concerns git-tracked
// artifacts carries the same labels.
const (
	tagGit           finding.Tag = "git"
	tagBinary        finding.Tag = "binary"
	tagBuildArtifact finding.Tag = "build-artifact"
	tagBestPractice  finding.Tag = "best-practice"
)

type ruleSpec struct {
	id          string
	name        string
	description string
	category    string
	severity    finding.Severity
	confidence  finding.Confidence
	tags        []finding.Tag
	message     func(fileInspection) string
	suggestion  func(string) string
	optIn       bool
}

var ruleTable = []*ruleSpec{ //nolint:gochecknoglobals // static rule registry
	{
		id:          RuleTrackedBinary,
		name:        "tracked binary file",
		description: "A git-tracked file contains binary data (NUL byte or low printable-text ratio). Build artifacts must not be committed.",
		category:    string(linter.CategoryBestPractice),
		severity:    finding.SeverityError,
		confidence:  finding.ConfidenceFull,
		tags:        []finding.Tag{tagGit, tagBinary, tagBuildArtifact, tagBestPractice},
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
	},
	{
		id:          RuleTrackedPattern,
		name:        "tracked binary artifact with text content",
		description: "A git-tracked file matches a binary artifact pattern but its content reads as text, which usually means an empty or placeholder artifact was committed.",
		category:    string(linter.CategoryBestPractice),
		severity:    finding.SeverityWarning,
		confidence:  finding.ConfidenceMedium,
		tags:        []finding.Tag{tagGit, tagBinary, tagBuildArtifact, tagBestPractice},
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
	},
	{
		id:          RuleTrackedIgnored,
		name:        "tracked file matches .gitignore",
		description: "A file is tracked even though .gitignore matches it. Git ignores apply only to untracked files, so the ignore rule is silently inert until the file is untracked.",
		category:    string(linter.CategoryConfiguration),
		severity:    finding.SeverityWarning,
		confidence:  finding.ConfidenceFull,
		tags:        []finding.Tag{"git", "repository-hygiene", "configuration"},
		optIn:       true,
	},
}

func ruleByID(id string) *ruleSpec {
	for _, rule := range ruleTable {
		if rule.id == id {
			return rule
		}
	}

	return nil
}

// AllRuleIDs returns every rule ID in registry order.
func AllRuleIDs() []string {
	ids := make([]string, 0, len(ruleTable))
	for _, rule := range ruleTable {
		ids = append(ids, rule.id)
	}

	return ids
}

// Rules builds the rule set over one shared Checker.
func Rules(checker *Checker) []linter.Rule {
	rules := make([]linter.Rule, 0, len(ruleTable))

	for _, spec := range ruleTable {
		rule := checkerRule{
			checker: checker,
			Meta: linter.RuleMeta{
				ID:          spec.id,
				Name:        spec.name,
				Description: spec.description,
				Cat:         linter.Category(spec.category),
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

type checkerRule struct {
	linter.RuleFunc

	checker *Checker
}

// Coverage reports how many tracked files the shared scan inspected.
func (r checkerRule) Coverage(ctx context.Context, dir string) (int, []string) {
	result, err := r.checker.Scan(ctx, dir)
	if err != nil || result == nil {
		return 0, nil
	}

	return result.FilesScanned, nil
}

type optInRule struct {
	checkerRule
}

func (optInRule) IsEnabledByDefault() bool { return false }

func ruleRunner(checker *Checker, ruleID string) func(context.Context, string) ([]finding.Finding, error) {
	return func(ctx context.Context, dir string) ([]finding.Finding, error) {
		result, err := checker.Scan(ctx, dir)
		if err != nil {
			return nil, err
		}

		findings := make([]finding.Finding, 0)

		for _, item := range result.Findings {
			if string(item.Rule) == ruleID {
				findings = append(findings, item)
			}
		}

		return findings, nil
	}
}
