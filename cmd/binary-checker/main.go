// Command binary-checker detects git-tracked binary files that should not be
// committed.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	errorfamily "github.com/larsartmann/go-error-family"
	"github.com/larsartmann/go-finding"
	linter "github.com/larsartmann/go-linter-sdk"

	binarycheck "github.com/larsartmann/binary-checker"
)

// Exit codes. 0, 1, and 2 are verdicts about findings; everything else means
// the tool could not deliver a verdict.
const (
	exitClean    = 0
	exitFindings = 1
	exitTriage   = 2

	// exitUsage reports a bad invocation: unknown flag or rule, bad threshold
	// or format, missing or invalid config or baseline, or a target that is
	// not a git work tree.
	exitUsage = 64
)

var version = "0.1.0"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

type options struct {
	dir              string
	format           string
	minConfidence    string
	enable           string
	disable          string
	configPath       string
	baselinePath     string
	saveBaselinePath string
	showSuppressed   bool
	showBaselined    bool
	listRules        bool
	printVersion     bool
}

func run(args []string, stdout, stderr io.Writer) int {
	opts, err := parseOptions(args, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitClean
		}

		return fail(stderr, err)
	}

	if opts.printVersion {
		if _, err := fmt.Fprintf(stdout, "binary-checker %s\n", version); err != nil {
			return fail(stderr, err)
		}

		return exitClean
	}

	return execute(opts, stdout, stderr)
}

// execute validates the invocation, runs the scan, and renders the report.
func execute(opts options, stdout, stderr io.Writer) int {
	threshold, err := parseThreshold(opts.minConfidence)
	if err != nil {
		return fail(stderr, err)
	}

	checker := binarycheck.NewChecker(binarycheck.WithConfigPath(opts.configPath))
	allRules := binarycheck.Rules(checker)

	selected, err := selectRules(allRules, opts.enable, opts.disable)
	if err != nil {
		return fail(stderr, err)
	}

	if opts.listRules {
		if err := writeRuleList(stdout, allRules); err != nil {
			return fail(stderr, err)
		}

		return exitClean
	}

	// The scan runs before the registry. Every rule reads the same memoized
	// result, so a failing scan is reported once instead of once per rule.
	scan, err := checker.Scan(context.Background(), opts.dir)
	if err != nil {
		return fail(stderr, err)
	}

	registry := linter.NewRegistryFromRules(selected, linter.WithToolName(binarycheck.ToolName))

	report, err := registry.Run(context.Background(), opts.dir)
	if err != nil {
		return fail(stderr, err)
	}

	return present(opts, stdout, stderr, report, scan, threshold)
}

// present partitions the findings, optionally records a baseline, renders
// the report, and returns the verdict's exit code.
func present(
	opts options,
	stdout, stderr io.Writer,
	report *finding.Report,
	scan *binarycheck.ScanResult,
	threshold finding.Confidence,
) int {
	base, err := loadBaseline(opts.baselinePath)
	if err != nil {
		return fail(stderr, err)
	}

	partitions := partition(report, threshold, base)

	if opts.saveBaselinePath != "" {
		if err := saveBaseline(opts.saveBaselinePath, report.ActiveFindings()); err != nil {
			return fail(stderr, err)
		}
	}

	if err := render(opts, stdout, partitions, scan); err != nil {
		return fail(stderr, err)
	}

	return verdictExit(partitions)
}

// fail reports an operational error and returns the exit code its error
// family maps to.
//
// Findings own exit codes 0, 1, and 2. go-error-family maps Rejection and
// Conflict to 1, which would read as "findings", so those become exitUsage.
func fail(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "Error: %v\n", err)

	code := errorfamily.ExitCode(err)
	if code == exitFindings {
		return exitUsage
	}

	return code
}

// verdictExit maps the finding partition to the process exit code: actionable
// findings fail the run, triage-only findings use the distinct triage code so
// CI can tell a policy decision from a clean tree.
func verdictExit(partitions reportPartitions) int {
	switch {
	case len(partitions.actionable) > 0:
		return exitFindings
	case len(partitions.triage) > 0:
		return exitTriage
	default:
		return exitClean
	}
}

func render(opts options, stdout io.Writer, partitions reportPartitions, scan *binarycheck.ScanResult) error {
	switch opts.format {
	case "human":
		return renderHuman(stdout, partitions, scan, opts.showSuppressed, opts.showBaselined)
	case "json":
		return renderJSON(stdout, partitions, scan, version)
	case "sarif":
		return renderSARIF(stdout, partitions)
	default:
		// parseOptions rejects unknown formats, so this is a programming error.
		return errorfamily.Newf(errorfamily.Orchestration, codeOutputRenderer,
			"format %q passed validation but has no renderer", opts.format)
	}
}

func parseOptions(args []string, stderr io.Writer) (options, error) {
	var opts options

	flags := flag.NewFlagSet("binary-checker", flag.ContinueOnError)
	flags.SetOutput(stderr)

	flags.StringVar(&opts.format, "format", "human", "output format: human, json, or sarif")
	flags.StringVar(&opts.minConfidence, "min-confidence", "high",
		"minimum confidence for actionable findings (low, medium, high, full)")
	flags.StringVar(&opts.enable, "enable", "", "comma-separated rule IDs to run (default: enabled rules only)")
	flags.StringVar(&opts.disable, "disable", "", "comma-separated rule IDs to skip")
	flags.StringVar(&opts.configPath, "config", "", "config file path (default: .binary-checker.json at the git root)")
	flags.StringVar(&opts.baselinePath, "baseline", "", "baseline file accepting previously recorded findings")
	flags.StringVar(&opts.saveBaselinePath, "save-baseline", "", "write current findings to a baseline file")
	flags.BoolVar(&opts.showSuppressed, "show-suppressed", false,
		"show findings silenced by config ignore entries")
	flags.BoolVar(&opts.showBaselined, "show-baselined", false, "show findings accepted by the baseline")
	flags.BoolVar(&opts.listRules, "list", false, "list the rules and exit")
	flags.BoolVar(&opts.printVersion, "version", false, "print the version and exit")

	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: binary-checker [flags] [directory]")
		fmt.Fprintln(stderr, "Detects git-tracked binary files that should not be committed.")
		fmt.Fprintln(stderr)
		flags.PrintDefaults()
	}

	if err := flags.Parse(args); err != nil {
		// flag.ErrHelp must stay reachable through errors.Is: `run` treats it as
		// a clean exit, not a failure.
		return opts, errorfamily.WrapRejectionf(err, codeCLIFlags, "parse flags")
	}

	switch count := flags.NArg(); count {
	case 0:
		opts.dir = "."
	case 1:
		opts.dir = flags.Arg(0)
	default:
		return opts, errorfamily.Newf(errorfamily.Rejection, codeCLIArgs,
			"expected at most one directory argument, got %d", count)
	}

	switch opts.format {
	case "human", "json", "sarif":
	default:
		return opts, errorfamily.Newf(errorfamily.Rejection, codeCLIFormat,
			"unknown format %q (want human, json, or sarif)", opts.format)
	}

	return opts, nil
}

func parseThreshold(value string) (finding.Confidence, error) {
	if value == "" {
		return finding.ConfidenceHigh, nil
	}

	threshold, err := finding.ParseConfidence(value)
	if err != nil {
		return 0, errorfamily.WrapRejectionf(err, codeCLIThreshold, "parse --min-confidence %q", value)
	}

	return threshold, nil
}

// selectRules applies --enable and --disable. Without --enable, only the
// rules enabled by default run; with it, exactly the named rules run.
func selectRules(all []linter.Rule, enable, disable string) ([]linter.Rule, error) {
	enableSet, disableSet := splitIDs(enable), splitIDs(disable)

	known := make(map[string]bool, len(all))
	for _, rule := range all {
		known[rule.ID()] = true
	}

	for _, set := range []map[string]bool{enableSet, disableSet} {
		for id := range set {
			if !known[id] {
				return nil, errorfamily.Newf(errorfamily.Rejection, codeCLIRule,
					"unknown rule %q (known: %s)", id, binarycheck.JoinRuleIDs(binarycheck.AllRuleIDs()))
			}
		}
	}

	var selected []linter.Rule
	if len(enableSet) > 0 {
		selected = linter.SelectRules(all, enableSet, disableSet)
	} else {
		for _, rule := range all {
			if rule.IsEnabledByDefault() && !disableSet[rule.ID()] {
				selected = append(selected, rule)
			}
		}
	}

	if len(selected) == 0 {
		return nil, errorfamily.Newf(errorfamily.Rejection, codeCLINoRules, "no rules selected")
	}

	return selected, nil
}

func splitIDs(value string) map[string]bool {
	result := make(map[string]bool)

	for part := range strings.SplitSeq(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			result[part] = true
		}
	}

	return result
}

func writeRuleList(writer io.Writer, rules []linter.Rule) error {
	if _, err := fmt.Fprintln(writer, "ID     DEFAULT  SEVERITY  DESCRIPTION"); err != nil {
		return errorfamily.WrapInfrastructuref(err, codeOutputWrite, "write rule list header")
	}

	for _, rule := range rules {
		defaultState := "off"
		if rule.IsEnabledByDefault() {
			defaultState = "on"
		}

		if _, err := fmt.Fprintf(writer, "%-6s %-8s %-9s %s\n",
			rule.ID(), defaultState, rule.Severity(), rule.Description()); err != nil {
			return errorfamily.WrapInfrastructuref(err, codeOutputWrite, "write rule list")
		}
	}

	return nil
}
