package binarycheck

import (
	"slices"
	"strings"

	errorfamily "github.com/larsartmann/go-error-family"
	"github.com/larsartmann/go-finding"
)

// SortFindings orders findings by position (path, line, column) and, within
// one position, by rule, so every output format is deterministic regardless
// of scan order.
//
// One file can carry findings from several rules (BC001 and BC003 share a
// position), and a comparator without the rule tie-break leaves their order
// to the sort algorithm.
func SortFindings(findings []finding.Finding) {
	slices.SortFunc(findings, func(left, right finding.Finding) int {
		if byPosition := left.Position.Compare(right.Position); byPosition != 0 {
			return byPosition
		}

		return strings.Compare(string(left.Rule), string(right.Rule))
	})
}

// verdictConfidence returns how sure a binary verdict is: a NUL byte is
// deterministic, the printable-text ratio is a heuristic.
func verdictConfidence(verdict ContentVerdict) finding.Confidence {
	if verdict.Evidence == EvidenceNulByte {
		return finding.ConfidenceFull
	}

	return finding.ConfidenceHigh
}

// buildFinding turns one offending inspection into a finding of the given
// rule, carrying the config suppression reason when the path is ignored.
func buildFinding(rule *ruleSpec, inspection fileInspection, suppressionReason string) (finding.Finding, error) {
	confidence := rule.confidence
	if rule.confidenceOf != nil {
		confidence = rule.confidenceOf(inspection)
	}

	builder := finding.NewBuilder(
		finding.RuleName(rule.id),
		finding.ToolName(ToolName),
		rule.message(inspection),
		rule.severity,
		finding.FilePos(finding.FilePath(inspection.file.Path)),
	).
		WithCategory(finding.Category(rule.category)).
		WithConfidence(confidence).
		WithFixStrategy(finding.FixStrategyNone).
		WithSuggestion(rule.suggestion(inspection.file.Path)).
		WithTags(rule.tags...)

	if rule.metadataOf != nil {
		builder = builder.WithMetadata(rule.metadataOf(inspection))
	}

	if suppressionReason != "" {
		builder = builder.WithSuppression(finding.Suppression{
			Kind:   finding.SuppressionInConfig,
			Rule:   finding.RuleName(rule.id),
			Reason: suppressionReason,
		})
	}

	item, err := builder.Build()
	if err != nil {
		return finding.Finding{}, errorfamily.WrapOrchestrationf(err, codeRuleBuildFailed,
			"build %s finding for %q", rule.id, inspection.file.Path)
	}

	return item, nil
}
