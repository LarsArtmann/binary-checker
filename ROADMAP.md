# Roadmap

Long-term direction, not yet refined into tasks.

- **Fleet integration parity.** Be the automatic check in every BuildFlow
  repository, replacing the deleted `binary-check` step and its ad-hoc
  scripts.
- **Distribution fronts.** One detector core behind CLI, BuildFlow provider,
  golangci-lint plugin, and GitHub Action.
- **Quality telemetry.** A health-score view over time: binary files removed,
  suppressed debt, baseline size — so the ratchet moves in the right
  direction.
- **Language-neutral artifact knowledge.** Extend the pattern table with
  ecosystem-specific build outputs (Gradle, Bazel, Xcode, .NET) while keeping
  the content check as the primary signal.
- **Not planned:** autofix. Deleting index entries is destructive and
  preference-laden; the tool recommends, the human decides.
