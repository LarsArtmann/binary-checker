// Package binarycheck implements a linter that detects git-tracked binary
// files which should not be committed.
//
// The detector is intentionally narrow: it examines ONLY files that git
// already tracks, because those are the files a "should not be committed"
// finding can act on. Untracked working-tree artifacts are not findings.
//
// Two independent signals classify a file:
//
//   - content: NUL bytes or a low printable-text ratio in the leading bytes
//   - path:    a curated binary-artifact extension or directory pattern
//
// A file carrying both signals produces one finding (BC001). A path match
// whose content looks like text produces a lower-confidence triage finding
// (BC002). Files tracked despite matching .gitignore are reported by the
// opt-in BC003 rule.
//
// False-positive controls shipped by default:
//
//   - Git LFS files are skipped (the pointer is text; the blob lives in LFS).
//   - Symlinks and submodule entries are skipped (no content to inspect).
//   - Web fonts and small images are exempt under documented size limits,
//     because those assets are legitimately committed.
//
// Tool name: "binary-check" (stable; configuration and suppressions key on it).
package binarycheck
