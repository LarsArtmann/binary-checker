# binary-checker — Agent Guide

Go linter that reports git-tracked binary files. Detector-only: it never
rewrites the index. Runs standalone and as a BuildFlow toolsdk provider.

## Commands

```bash
GOEXPERIMENT=jsonv2 go test -race ./...
GOEXPERIMENT=jsonv2 go vet ./...
GOEXPERIMENT=jsonv2 go run ./cmd/binary-checker .
nix build .
```

## Layout

| Path                         | Purpose                                                        |
| ---------------------------- | -------------------------------------------------------------- |
| `binary.go`                  | Content heuristic (NUL byte or printable ratio)                |
| `patterns.go`                | Binary artifact, font, and small-image pattern tables          |
| `glob.go`                    | `**`-aware path matching used by patterns and config ignores   |
| `git.go`                     | Read-only git queries: index, tracked-but-ignored, LFS attrs   |
| `config.go`                  | `.binary-checker.json` schema, defaults, validation            |
| `scan.go`                    | Checker: git walk, parallel inspection, finding construction   |
| `rules.go`                   | Stable rule IDs, `RuleFunc` wiring, registry construction      |
| `pkg/provider/provider.go`   | `toolsdk.Register` — BuildFlow self-registration               |
| `cmd/binary-checker/`        | CLI: flags, human/JSON/SARIF output, baseline                  |

## Invariants

- **Rule IDs are API.** `BC001`, `BC002`, `BC003` never change. Ignore
  entries, baselines, and suppressions key on them.
- **Tracked files only.** The actionable universe is the git index. Untracked
  artifacts are out of scope.
- **Multi-signal.** BC001 = tracked + content binary. BC002 = tracked +
  artifact pattern + text content. Never emit a finding from one signal alone.
- **Reason-required ignores.** An ignore entry without a reason is a config
  error, not a warning.
- **Suppressed findings stay in the report** as `in-config` suppression data.
  The CLI hides them by default; the BuildFlow detector filters them so the
  severity gate only counts actionable findings.
- **Deterministic output.** Sort findings by path, line, rule before rendering.
- **Dry-run/repair contract does not apply.** There is no repairer; do not add
  one without reading BuildFlow's policy-vs-mechanical rule. Untracking files
  is a preference-laden, destructive action.

## Testing discipline

- Every rule has positive and negative fixtures (`scanner_test.go`).
- Global git config is isolated in tests; never rely on the developer's config.
- Mutation-proof the suite when changing detection: revert the fix (or plant a
  broken predicate), confirm the exact symptom fails, restore, confirm green.
  A compile-clean mutant is required; an unused-import failure proves nothing.
- Provider tests assert the V2 coverage path so BuildFlow can distinguish
  "clean tree" from "scanned nothing".

## Release and BuildFlow pin (durable integration)

BuildFlow consumes this repo as a flake input + prepared-source dependency:

1. Tag `vX.Y.Z` on a green commit and push.
2. In BuildFlow `flake.nix`: add
   `binary-checker = { url = "git+ssh://git@github.com/LarsArtmann/binary-checker?ref=refs/tags/vX.Y.Z"; flake = false; };`
3. Add `"github.com/larsartmann/binary-checker" = inputs.binary-checker;` to
   `preparedSrc.deps` (root module carries `pkg/provider`, so no `subModules`
   entry is needed).
4. Add the require to `tools/go.mod` (and root `go.mod` indirect) at the tag
   version, plus the blank import in `tools/providers/sdk_imports.go`.
5. `nix run .#update-vendor-hash`; run `nix build .` and the sdk import
   regression test.

Never commit a local-path replace for this module: BuildFlow's audit gate
rejects path replaces outside the repo root (gotcha #286).
