package binarycheck

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"strings"

	errorfamily "github.com/larsartmann/go-error-family"
)

const codeGit = "binary_checker.git"

// lsStageFields is the number of whitespace-separated fields `git ls-files
// --stage` emits before the tab-delimited path: mode, object id, and stage.
const lsStageFields = 3

// ErrNotGitRepository reports that the scan target is not inside a git work
// tree. Binary checking is defined over git's index, so there is nothing
// honest to scan without one.
var ErrNotGitRepository error = errorfamily.NewInfrastructure(
	codeGit, "not a git repository (or any parent up to the filesystem root)",
)

// trackedFile is one stage-0 entry from the git index.
type trackedFile struct {
	// Path is repository-relative and slash-separated.
	Path string
	// Mode is the index mode ("100644", "100755", "120000", "160000").
	Mode string
}

// gitClient wraps read-only git queries rooted at a work tree.
type gitClient struct {
	root string
}

// newGitClient discovers the work-tree root for dir.
func newGitClient(ctx context.Context, dir string) (*gitClient, error) {
	root, err := discoverGitRoot(ctx, dir)
	if err != nil {
		return nil, err
	}

	return &gitClient{root: root}, nil
}

func discoverGitRoot(ctx context.Context, dir string) (string, error) {
	output, err := runGit(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", errorfamily.WrapInfrastructuref(
			err, codeGit, "not a git repository (or any parent up to %q)", dir,
		)
	}

	root := strings.TrimSpace(string(output))
	if root == "" {
		return "", errorfamily.WrapInfrastructuref(
			ErrNotGitRepository, codeGit, "not a git repository (or any parent up to %q)", dir,
		)
	}

	return filepath.Clean(root), nil
}

// tracked returns every stage-0 file in the index and the number of index
// entries that carry no inspectable content (symlinks and submodules).
func (g *gitClient) tracked(ctx context.Context) ([]trackedFile, int, error) {
	output, err := runGit(ctx, g.root, "ls-files", "--stage", "-z")
	if err != nil {
		return nil, 0, errorfamily.WrapInfrastructuref(err, codeGit, "list tracked files")
	}

	var files []trackedFile

	seen := make(map[string]bool)
	skipped := make(map[string]bool)

	for record := range bytes.SplitSeq(output, []byte{0}) {
		if len(record) == 0 {
			continue
		}

		before, after, ok := bytes.Cut(record, []byte{'\t'})
		if !ok {
			continue
		}

		fields := strings.Fields(string(before))
		if len(fields) < lsStageFields {
			continue
		}

		mode, stage, path := fields[0], fields[2], string(after)
		if stage != "0" || seen[path] {
			continue
		}

		seen[path] = true

		if mode == "120000" || mode == "160000" {
			skipped[path] = true

			continue
		}

		files = append(files, trackedFile{Path: path, Mode: mode})
	}

	return files, len(skipped), nil
}

// trackedIgnored returns files that are tracked despite matching .gitignore.
func (g *gitClient) trackedIgnored(ctx context.Context) ([]string, error) {
	output, err := runGit(ctx, g.root, "ls-files", "--cached", "--ignored", "--exclude-standard", "-z")
	if err != nil {
		return nil, errorfamily.WrapInfrastructuref(err, codeGit, "list tracked-but-ignored files")
	}

	var paths []string

	for record := range bytes.SplitSeq(output, []byte{0}) {
		if len(record) > 0 {
			paths = append(paths, string(record))
		}
	}

	return paths, nil
}

// lfsPaths returns the subset of paths whose `filter` attribute is lfs.
//
// The check runs through `git check-attr`, which applies the same attribute
// resolution git itself uses (nested .gitattributes files, pattern precedence).
func (g *gitClient) lfsPaths(ctx context.Context, paths []string) (map[string]bool, error) {
	if len(paths) == 0 {
		return nil, nil
	}

	input := strings.Join(paths, "\x00") + "\x00"

	output, err := runGitWithInput(ctx, g.root, input, "check-attr", "-z", "--stdin", "filter")
	if err != nil {
		// Attribute lookup is an FP-control nicety. A git version that cannot
		// answer must not fail the whole scan; fall back to no LFS knowledge.
		return nil, nil //nolint:nilerr // deliberate degradation: LFS detection is advisory
	}

	result := make(map[string]bool)
	fields := bytes.Split(output, []byte{0})

	for i := 0; i+2 < len(fields); i += 3 {
		path := string(fields[i])
		if string(fields[i+2]) == "lfs" {
			result[path] = true
		}
	}

	return result, nil
}

func runGit(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return runGitWithInput(ctx, dir, "", args...)
}

func runGitWithInput(ctx context.Context, dir, input string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...) //nolint:gosec // fixed git subcommand, paths are repository data
	command.Env = append(command.Environ(), "LC_ALL=C")

	if input != "" {
		command.Stdin = strings.NewReader(input)
	}

	output, err := command.Output()
	if err != nil {
		return nil, errorfamily.WrapInfrastructuref(err, codeGit, "git %s", strings.Join(args, " "))
	}

	return output, nil
}
