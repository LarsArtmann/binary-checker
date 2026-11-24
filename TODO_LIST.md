# TODO List

Actionable, bounded work.

## Now

- [ ] Publish `github.com/larsartmann/binary-checker` and tag `v0.1.0`.
- [ ] Wire the tag into BuildFlow: flake input, prepared-source `deps` entry,
      `tools/go.mod` require, `sdk_imports.go` blank import, vendor hash,
      and the sdk import regression test.
- [ ] Verify `nix build .` and compute the final `vendorHash`.

## Next

- [ ] Add a golangci-lint v2 plugin front over the same rules
      (`plugin/plugin.go`, `.custom-gcl.yml`).
- [ ] Add a GitHub Action (`action.yml`) that installs the CLI and uploads
      SARIF.
- [ ] Add a corpus sweep: run against 20+ fleet repositories, triage every
      finding, record the false-positive rate per rule in `docs/validation/`.

## Later

- [ ] Consider `--include-untracked` for pre-`git add` hygiene.
- [ ] Consider artifact-family grouping for very large repositories.
- [ ] Add fuzz tests for the config decoder and glob matcher.
