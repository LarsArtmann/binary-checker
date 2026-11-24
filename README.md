# binary-checker

**Find binary files that should not be in git — before they are cloned forever.**

A binary blob committed to git is cloned and fetched by every future clone, forever. `binary-checker` scans the **git index** (not the working tree) and reports tracked binary files with actionable, confidence-scored findings.

It runs standalone, or automatically inside any [BuildFlow](https://github.com/larsartmann/buildflow)-covered repository via the `binary-check` step.

## Why this tool exists

The original BuildFlow `binary-check` step was deleted in the v3 migration. This is its modern replacement, rebuilt on the shared linter stack:

- **Shared finding model** — `github.com/larsartmann/go-finding` (no private issue type, no converter layer)
- **Shared rule scaffolding** — `github.com/larsartmann/go-linter-sdk`
- **Self-registering BuildFlow provider** — `go-finding/toolsdk`

## Install

```bash
go install github.com/larsartmann/binary-checker/cmd/binary-checker@latest
```

Or use it through BuildFlow, which discovers the provider automatically:

```bash
buildflow -s binary-check
```

## Usage

```bash
binary-checker                # scan the current repository
binary-checker path/to/repo   # scan another repository
binary-checker --format json   # machine output
binary-checker --format sarif  # GitHub code-scanning upload
binary-checker --list          # list rules and defaults
```

Exit codes follow the ecosystem convention:

| Code | Meaning                                                           |
| ---- | ----------------------------------------------------------------- |
| 0    | Clean (suppressed and baselined findings do not fail the run)     |
| 1    | Findings at or above `--min-confidence` (default: `high`)         |
| 2    | Only lower-confidence findings (triage), or the tool could not run |

## Rules

| ID    | Default | Severity | Confidence        | What it reports                                                                 |
| ----- | ------- | -------- | ----------------- | ------------------------------------------------------------------------------- |
| BC001 | on      | error    | full (NUL byte) / high (text ratio) | A tracked file's content is binary.                        |
| BC002 | on      | warning  | medium            | A tracked file matches a binary artifact pattern but reads as text.             |
| BC003 | **off** | warning  | full              | A tracked file also matches `.gitignore` (ignore rules do not apply to tracked files). |

BC003 is opt-in because it is a different concern from binary content:

```bash
binary-checker --enable BC003
```

## Configuration

`binary-checker` auto-discovers `.binary-checker.json` at the git root.

```json
{
  "ignore": [
    { "path": "testdata/fixtures/**", "reason": "binary fixture corpus, intentionally committed" },
    { "path": "assets/brand.woff2", "reason": "brand font served from the repo", "rules": ["BC001"] }
  ],
  "allow_fonts": true,
  "font_max_bytes": 524288,
  "allow_small_images": true,
  "small_image_max_bytes": 1048576
}
```

Rules for the config:

- Every `ignore` entry **requires a reason**. An unexplained ignore outlives its justification.
- Unknown fields are rejected — a typo cannot silently do nothing.
- `rules` scopes an ignore to specific rule IDs; omit it to suppress all rules.
- Set `allow_fonts` / `allow_small_images` to `false` to report those assets too.

## False-positive controls

The tool is deliberately conservative:

- **Tracked files only.** Untracked build output is not a finding; it is not in git.
- **Two signals.** Content analysis plus artifact-pattern matching. A path match alone is only a triage finding (BC002).
- **Git LFS files are skipped.** The blob lives in LFS; the pointer file is text.
- **Symlinks and submodules are skipped.** There is no content to inspect.
- **Small images and web fonts are exempt** under documented size limits, because those are legitimate repository assets.
- **Deterministic ordering** by path, line, and rule, so JSON/SARIF diffs are stable.
- **Baseline support** (`--save-baseline` / `--baseline`) for incremental adoption on an existing repository.
- **Config suppressions are data**, attached to findings as `in-config` suppressions, not silently dropped.

## How it works

```
git index ──► candidate files ──► content sample (NUL/UTF-8 ratio)
                                    │
                    artifact pattern┘
                                    ▼
                            go-finding findings
                              ├─ CLI (human, JSON, SARIF, baseline)
                              └─ BuildFlow toolsdk provider (binary-check)
```

The CLI and the BuildFlow provider share one detector core — no second implementation.

## Development

```bash
GOEXPERIMENT=jsonv2 go test -race ./...
GOEXPERIMENT=jsonv2 go vet ./...
nix build .
```

Tests create throwaway git repositories; global git config is isolated (`GIT_CONFIG_GLOBAL`, `GIT_CONFIG_NOSYSTEM`) so a developer's global ignore rules cannot change test outcomes.

## Limitations

- Requires `git` on `PATH`; the check is defined over the index.
- The content heuristic inspects the first 8,000 bytes, like git's own binary detection.
- No autofix: removing a file from the index is a destructive operation and stays a human decision.

## License

MIT
