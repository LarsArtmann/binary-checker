package main

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"

	errorfamily "github.com/larsartmann/go-error-family"
	"github.com/larsartmann/go-finding"

	"github.com/larsartmann/binary-checker/pkg/binarycheck"
)

type reportPartitions struct {
	actionable []finding.Finding
	triage     []finding.Finding
	suppressed []finding.Finding
	baselined  []finding.Finding
}

// partition sorts the report's findings into the groups the CLI shows. The
// groups are exclusive and checked in order of certainty: a suppression wins
// over a baseline entry, which wins over the confidence threshold.
func partition(report *finding.Report, threshold finding.Confidence, base *baseline) reportPartitions {
	partitions := reportPartitions{
		actionable: make([]finding.Finding, 0),
		triage:     make([]finding.Finding, 0),
		suppressed: make([]finding.Finding, 0),
		baselined:  make([]finding.Finding, 0),
	}

	for _, item := range report.FindingsSnapshot() {
		switch {
		case item.IsSuppressed():
			partitions.suppressed = append(partitions.suppressed, item)
		case base.contains(item):
			partitions.baselined = append(partitions.baselined, item)
		case item.Confidence.Compare(threshold) >= 0:
			partitions.actionable = append(partitions.actionable, item)
		default:
			partitions.triage = append(partitions.triage, item)
		}
	}

	for _, items := range [][]finding.Finding{
		partitions.actionable, partitions.triage, partitions.suppressed, partitions.baselined,
	} {
		binarycheck.SortFindings(items)
	}

	return partitions
}

// renderHuman writes the report people read. It is composed in memory and
// written in one step, like the JSON and SARIF renderers, so a failed write
// surfaces as an error instead of a half-printed report.
func renderHuman(
	writer io.Writer,
	partitions reportPartitions,
	scan *binarycheck.ScanResult,
	showSuppressed, showBaselined bool,
) error {
	var report bytes.Buffer

	if len(partitions.actionable) == 0 && len(partitions.triage) == 0 &&
		(!showSuppressed || len(partitions.suppressed) == 0) &&
		(!showBaselined || len(partitions.baselined) == 0) {
		fmt.Fprintln(&report, "No actionable findings.")

		if len(partitions.baselined) > 0 {
			fmt.Fprintf(&report, "%d finding(s) accepted by the baseline (use --show-baselined).\n",
				len(partitions.baselined))
		}

		if len(partitions.suppressed) > 0 {
			fmt.Fprintf(&report, "%d finding(s) suppressed by config (use --show-suppressed).\n",
				len(partitions.suppressed))
		}
	}

	writeSection(&report, "ACTIONABLE", partitions.actionable)
	writeSection(&report, "TRIAGE (low confidence)", partitions.triage)

	if showBaselined {
		writeSection(&report, "BASELINED", partitions.baselined)
	}

	if showSuppressed {
		writeSection(&report, "SUPPRESSED", partitions.suppressed)
	}

	scanned, exempt := 0, 0
	if scan != nil {
		scanned, exempt = scan.FilesScanned, scan.ExemptFiles
	}

	fmt.Fprintf(&report,
		"\n%d tracked file(s) scanned; %d actionable, %d triage, %d baselined, %d suppressed, %d exempt asset(s)\n",
		scanned,
		len(partitions.actionable),
		len(partitions.triage),
		len(partitions.baselined),
		len(partitions.suppressed),
		exempt,
	)

	if _, err := writer.Write(report.Bytes()); err != nil {
		return errorfamily.WrapInfrastructuref(err, codeOutputWrite, "write human report")
	}

	return nil
}

func writeSection(report *bytes.Buffer, title string, findings []finding.Finding) {
	if len(findings) == 0 {
		return
	}

	fmt.Fprintf(report, "\n%s (%d)\n", title, len(findings))

	for _, item := range findings {
		location := string(item.Position.File)
		if item.Position.Line > 0 {
			location = fmt.Sprintf("%s:%d", item.Position.File, item.Position.Line)
		}

		fmt.Fprintf(report, "  %s  %-7s %-6s  %s  %s\n",
			location, item.Severity, item.Confidence, item.Rule, item.Message)

		if item.Suggestion != "" {
			fmt.Fprintf(report, "      fix: %s\n", item.Suggestion)
		}
	}
}

type jsonReport struct {
	Tool         string            `json:"tool"`
	Version      string            `json:"version"`
	FilesScanned int               `json:"files_scanned"`
	FilesExempt  int               `json:"files_exempt"`
	Actionable   []finding.Finding `json:"actionable"`
	Triage       []finding.Finding `json:"triage"`
	Baselined    []finding.Finding `json:"baselined"`
	Suppressed   []finding.Finding `json:"suppressed"`
}

func renderJSON(writer io.Writer, partitions reportPartitions, scan *binarycheck.ScanResult, version string) error {
	output := jsonReport{
		Tool:       binarycheck.ToolName,
		Version:    version,
		Actionable: partitions.actionable,
		Triage:     partitions.triage,
		Baselined:  partitions.baselined,
		Suppressed: partitions.suppressed,
	}

	if scan != nil {
		output.FilesScanned = scan.FilesScanned
		output.FilesExempt = scan.ExemptFiles
	}

	data, err := json.Marshal(output, jsontext.WithIndentPrefix(""), jsontext.WithIndent("  "))
	if err != nil {
		return errorfamily.WrapOrchestrationf(err, codeOutputEncode, "encode JSON report")
	}

	if _, err := fmt.Fprintf(writer, "%s\n", data); err != nil {
		return errorfamily.WrapInfrastructuref(err, codeOutputWrite, "write JSON report")
	}

	return nil
}

func renderSARIF(writer io.Writer, partitions reportPartitions) error {
	displayed := make([]finding.Finding, 0, len(partitions.actionable)+len(partitions.triage))
	displayed = append(displayed, partitions.actionable...)
	displayed = append(displayed, partitions.triage...)

	report := finding.NewReportFromFindings(
		finding.ToolInfo{Name: binarycheck.ToolName, Version: version},
		displayed,
	)

	data, err := report.ToSARIF()
	if err != nil {
		return errorfamily.WrapOrchestrationf(err, codeOutputEncode, "encode SARIF report")
	}

	if _, err := writer.Write(data); err != nil {
		return errorfamily.WrapInfrastructuref(err, codeOutputWrite, "write SARIF report")
	}

	if _, err := fmt.Fprintln(writer); err != nil {
		return errorfamily.WrapInfrastructuref(err, codeOutputWrite, "write SARIF report")
	}

	return nil
}
