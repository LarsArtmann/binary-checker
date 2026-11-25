package main

// Error codes of the CLI: one per condition, shaped
// "<tool>.<area>.<condition>", matching the library package. go-error-family
// matches errors.Is by code AND family, so conditions must not share a code.
const (
	codeCLIArgs      = "binary_checker.cli.args"
	codeCLIFlags     = "binary_checker.cli.flags"
	codeCLIFormat    = "binary_checker.cli.format"
	codeCLINoRules   = "binary_checker.cli.no_rules"
	codeCLIRule      = "binary_checker.cli.rule_unknown"
	codeCLIThreshold = "binary_checker.cli.threshold"

	codeBaselineEncode     = "binary_checker.baseline.encode"
	codeBaselineParse      = "binary_checker.baseline.parse"
	codeBaselineRead       = "binary_checker.baseline.read"
	codeBaselineReadFailed = "binary_checker.baseline.read_failed"
	codeBaselineTool       = "binary_checker.baseline.tool_mismatch"
	codeBaselineVersion    = "binary_checker.baseline.version_unsupported"
	codeBaselineWrite      = "binary_checker.baseline.write"

	codeOutputEncode   = "binary_checker.output.encode"
	codeOutputRenderer = "binary_checker.output.renderer_missing"
	codeOutputWrite    = "binary_checker.output.write"
)
