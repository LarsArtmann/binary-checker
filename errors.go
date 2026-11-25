package binarycheck

import (
	errorfamily "github.com/larsartmann/go-error-family"
)

// Error codes: one per condition, shaped "<tool>.<area>.<condition>".
//
// go-error-family matches errors.Is by code AND family, so a code shared by a
// whole area would make every error of that area equal to every other and
// errors.Is useless for telling conditions apart. The tool prefix keeps codes
// unique inside a process that hosts several tools.
const (
	codeConfigInvalid      = "binary_checker.config.invalid"
	codeConfigOpen         = "binary_checker.config.open"
	codeConfigParse        = "binary_checker.config.parse"
	codeConfigPath         = "binary_checker.config.path"
	codeConfigTrailingData = "binary_checker.config.trailing_data"

	codeFileNotRegular = "binary_checker.file.not_regular"

	codeGitCancelled     = "binary_checker.git.cancelled"
	codeGitFailed        = "binary_checker.git.failed"
	codeGitMissing       = "binary_checker.git.missing"
	codeGitNotRepository = "binary_checker.git.not_a_repository"

	codeRuleBuildFailed = "binary_checker.rule.build_failed"

	codeScanInterrupted = "binary_checker.scan.interrupted"
)

// ErrNotGitRepository reports that the scan target is not inside a git work
// tree (or is a bare repository). Binary checking is defined over git's index,
// so there is nothing honest to scan without one. It is a Rejection: the
// target is the caller's input, and retrying cannot help.
var ErrNotGitRepository error = errorfamily.NewRejection(
	codeGitNotRepository, "not a git repository (or any parent up to the filesystem root)",
)

// ErrGitMissing reports that no git executable is available. Every query the
// checker makes goes through git, so there is no fallback.
var ErrGitMissing error = errorfamily.NewInfrastructure(
	codeGitMissing, "git is not available on PATH",
)

// ErrNotRegularFile reports that a path is not a regular file (a directory or
// symlink), so it has no content of its own to inspect.
var ErrNotRegularFile error = errorfamily.NewRejection(codeFileNotRegular, "not a regular file")
