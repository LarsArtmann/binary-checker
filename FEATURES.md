# Features

Honest inventory by status.

## DONE

- Tracked-file scan via `git ls-files --stage` (stage-0 only, symlinks and
  submodules excluded).
- Content-based binary detection (NUL byte, printable-text ratio) over the
  first 8,000 bytes.
- Binary artifact pattern matching (executables, archives, media, documents,
  databases, object files).
- BC001 / BC002 / BC003 rules with stable IDs, severity, and per-finding
  confidence.
- Git LFS skip via `git check-attr filter`.
- Small-image and web-font size exemptions (configurable).
- Reason-required `.binary-checker.json` ignore entries, rule-scoped.
- Human, JSON, and SARIF output with deterministic ordering.
- Confidence threshold and rule enable/disable flags.
- Baseline save/accept for incremental adoption.
- BuildFlow toolsdk provider with `DetectorV2` coverage and suppressed-finding
  filtering.
- Unit and integration tests, including real throwaway git repositories and a
  mutation-proofed detection suite.
- Nix packaging: `nix build` compiles the binary _and_ runs the whole test
  suite in the sandbox; `vendorHash` is pinned for the current dependency set.
- golangci-lint gate (36 linters, gofumpt/gci/goimports formatters) at zero
  findings, plus erraudit error-handling audit at zero violations.
- CI on both hosts (`.forgejo/workflows/ci.yml`, `.github/workflows/ci.yml`):
  build, vet, race tests, lint, nix build.

## PARTIALLY DONE

- Nothing outstanding.

## PLANNED

- Publish the repository and tag `v0.1.0`, then pin it in BuildFlow's flake
  inputs + prepared-source dependency map.
- golangci-lint v2 module plugin front (the detector core is already isolated).
- GitHub Action wrapper.

## WORTH CONSIDERING

- Optional working-tree mode (`--include-untracked`) for pre-add hygiene.
- `.gitattributes`-driven custom exemption patterns.
- Finding grouping by artifact family for large repositories.
