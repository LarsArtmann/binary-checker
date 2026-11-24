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

	"github.com/larsartmann/go-finding"
	linter "github.com/larsartmann/go-linter-sdk"

	binarycheck "github.com/larsartmann/binary-checker"
)

const (
	exitClean       = 0
	exitFindings    = 1
	exitTriage      = 2
	exitOperational = 2
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

		return exitOperational
	}

	if opts.printVersion {
		fmt.Fprintf(stdout, "binary-checker %s\n", version)

		return exitClean
	}

	checker := binarycheck.NewChecker(binarycheck.WithConfigPath(opts.configPath))
	allRules := binarycheck.Rules(checker)

	if opts.listRules {
		writeRuleList(stdout, allRules)

		return exitClean
	}

	threshold, err := parseThreshold(opts.minConfidence)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)

		return exitOperational
	}

	selected, err := selectRules(allRules, opts.enable, opts.disable)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)

		return exitOperational
	}

	registry := linter.NewRegistryFromRules(selected, linter.WithToolName(binarycheck.ToolName))

	report, err := registry.Run(context.Background(), opts.dir, linter.ContinueOnError())
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)

		return exitOperational
	}

	scan, err := checker.Scan(context.Background(), opts.dir)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)

		return exitOperational
	}

	base, err := loadBaseline(opts.baselinePath)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)

		return exitOperational
	}

	partitions := partition(report, threshold, base)

	if opts.saveBaselinePath != "" {
		if err := saveBaseline(opts.saveBaselinePath, report.ActiveFindings()); err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)

			return exitOperational
		}
	}

	if err := render(opts, stdout, partitions, scan); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)

		return exitOperational
	}

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
		renderHuman(stdout, partitions, scan, opts.showSuppressed, opts.showBaselined)

		return nil
	case "json":
		return renderJSON(stdout, partitions, scan, version)
	case "sarif":
		return renderSARIF(stdout, partitions)
	default:
		return fmt.Errorf("unknown format %q (want human, json, or sarif)", opts.format)
	}
}

func parseOptions(args []string, stderr io.Writer) (options, error) {
	var opts options

	flags := flag.NewFlagSet("binary-checker", flag.ContinueOnError)
	flags.SetOutput(stderr)

	flags.StringVar(&opts.format, "format", "human", "output format: human, json, or sarif")
	flags.StringVar(&opts.minConfidence, "min-confidence", "high", "minimum confidence for actionable findings (low, medium, high, full)")
	flags.StringVar(&opts.enable, "enable", "", "comma-separated rule IDs to run (default: enabled rules only)")
	flags.StringVar(&opts.disable, "disable", "", "comma-separated rule IDs to skip")
	flags.StringVar(&opts.configPath, "config", "", "config file path (default: .binary-checker.json at the git root)")
	flags.StringVar(&opts.baselinePath, "baseline", "", "baseline file accepting previously recorded findings")
	flags.StringVar(&opts.saveBaselinePath, "save-baseline", "", "write current findings to a baseline file")
	flags.BoolVar(&opts.showSuppressed, "show-suppressed", false, "show findings silenced by config ignore entries")
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
		return opts, err
	}

	switch flags.NArg() {
	case 0:
		opts.dir = "."
	case 1:
		opts.dir = flags.Arg(0)
	default:
		return opts, errors.New("expected at most one directory argument")
	}

	switch opts.format {
	case "human", "json", "sarif":
	default:
		return opts, fmt.Errorf("unknown format %q (want human, json, or sarif)", opts.format)
	}

	return opts, nil
}

func parseThreshold(value string) (finding.Confidence, error) {
	if value == "" {
		return finding.ConfidenceHigh, nil
	}

	threshold, err := finding.ParseConfidence(value)
	if err != nil {
		return 0, fmt.Errorf("parse --min-confidence %q: %w", value, err)
	}

	return threshold, nil
}

func selectRules(all []linter.Rule, enable, disable string) ([]linter.Rule, error) {
	enableSet := splitIDs(enable)
	disableSet := splitIDs(disable)

	known := make(map[string]bool, len(all))
	for _, rule := range all {
		known[rule.ID()] = true
	}

	for id := range enableSet {
		if !known[id] {
			return nil, fmt.Errorf("unknown rule %q (known: %s)", id, strings.Join(binarycheck.AllRuleIDs(), ", "))
		}
	}

	for id := range disableSet {
		if !known[id] {
			return nil, fmt.Errorf("unknown rule %q (known: %s)", id, strings.Join(binarycheck.AllRuleIDs(), ", "))
		}
	}

	selected := make([]linter.Rule, 0, len(all))

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
		return nil, errors.New("no rules selected")
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

func writeRuleList(writer io.Writer, rules []linter.Rule) {
	fmt.Fprintln(writer, "ID     DEFAULT  SEVERITY  DESCRIPTION")

	for _, rule := range rules {
		defaultState := "off"
		if rule.IsEnabledByDefault() {
			defaultState = "on"
		}

		fmt.Fprintf(
			writer,
			"%-6s %-8s %-9s %s\n",
			rule.ID(),
			defaultState,
			rule.Severity(),
			rule.Description(),
		)
	}
}
