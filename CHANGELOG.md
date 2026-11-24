# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-11-25

### Added

- Modern rebuild of the deleted BuildFlow `binary-check` step as a standalone
  linter built on `go-finding` and `go-linter-sdk`.
- Rules `BC001` (tracked binary content), `BC002` (binary artifact pattern
  with text content), and opt-in `BC003` (tracked file matching `.gitignore`).
- Git LFS, symlink, submodule, small-image, and web-font exemptions.
- `.binary-checker.json` configuration with reason-required ignore entries,
  strict unknown-field rejection, and rule-scoped suppressions.
- CLI with human, JSON, and SARIF output, confidence thresholds, rule
  selection, and baseline save/accept.
- `pkg/provider` self-registration for BuildFlow via
  `go-finding/toolsdk`, including the detector V2 coverage contract.
- Tests: content heuristic, glob semantics, config validation, scanner
  fixtures against real throwaway git repositories, registry behavior, CLI
  exit codes/formats/baseline, and provider suppression filtering.

### Notes

- Detector-only: no autofix. Untracking files is deliberately a human action.
