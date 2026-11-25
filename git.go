package binarycheck

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	errorfamily "github.com/larsartmann/go-error-family"
)

// Error codes name one condition each, in the fleet's "<area>.<condition>"
// shape. go-error-family matches errors.Is by code AND family, so a code
// shared by a whole layer would make every git failure look like
// ErrNotGitRepository.
const (
	codeGitCancelled         = "git.cancelled"
	codeGitIndexUnreadable   = "git.index_unreadable"
	codeGitIgnoredUnreadable = "git.ignored_unreadable"
	codeGitNotRepository     = "git.not_a_repository"
)

// lsStageFields is the number of whitespace-separated fields `git ls-files
// --stage` emits before the tab-delimited path: mode, object id, and stage.
const lsStageFields = 3

// ErrNotGitRepository reports that the scan target is not inside a git work
// tree. Binary checking is defined over git's index, so there is nothing
// honest to scan without one.
var ErrNotGitRepository error = errorfamily.NewInfrastructure(
	codeGitNotRepository, "not a git repository (or any parent up to the filesystem root)",
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
		return "", classifyGit(ctx, err, errorfamily.Infrastructure, codeGitNotRepository,
			"not a git repository (or any parent up to %q)", dir)
	}

	root := strings.TrimSpace(string(output))
	if root == "" {
		return "", errorfamily.WrapInfrastructuref(
			ErrNotGitRepository, codeGitNotRepository, "not a git repository (or any parent up to %q)", dir,
		)
	}

	return filepath.Clean(root), nil
}

// tracked returns every stage-0 file in the index and the number of index
// entries that carry no inspectable content (symlinks and submodules).
func (g *gitClient) tracked(ctx context.Context) ([]trackedFile, int, error) {
	output, err := runGit(ctx, g.root, "ls-files", "--stage", "-z")
	if err != nil {
		return nil, 0, classifyGit(ctx, err, errorfamily.Infrastructure, codeGitIndexUnreadable,
			"list tracked files in %q", g.root)
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

// trackedIgnored returns files that are tracked despite matching a .gitignore
// file in the repository.
//
// Only the repository's own ignore rules count. `--exclude-standard` would
// also apply .git/info/exclude and the user's global ignore file, which are
// personal to one clone: the same commit would then produce different
// findings on different machines.
func (g *gitClient) trackedIgnored(ctx context.Context) ([]string, error) {
	output, err := runGit(ctx, g.root,
		"ls-files", "--cached", "--ignored", "--exclude-per-directory=.gitignore", "-z")
	if err != nil {
		return nil, classifyGit(ctx, err, errorfamily.Infrastructure, codeGitIgnoredUnreadable,
			"list tracked-but-ignored files in %q", g.root)
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
// Attribute lookup is an FP-control nicety, so a git that cannot answer
// degrades to no LFS knowledge rather than failing the scan. A cancelled
// context is the exception: it is the caller ending the scan, not git
// failing to answer, and must not be mistaken for "no LFS files here".
func (g *gitClient) lfsPaths(ctx context.Context, paths []string) (map[string]bool, error) {
	if len(paths) == 0 {
		return nil, nil
	}

	input := strings.Join(paths, "\x00") + "\x00"

	output, err := runGitWithInput(ctx, g.root, input, "check-attr", "-z", "--stdin", "filter")
	if err != nil {
		if ctx.Err() != nil {
			return nil, classifyGit(ctx, err, errorfamily.Infrastructure, "git.lfs_attributes_unreadable",
				"look up Git LFS attributes in %q", g.root)
		}

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

// gitCommandError is a failed git invocation, left unclassified on purpose:
// only the caller knows which condition the failure represents. It carries
// git's stderr, because "exit status 128" alone never tells a user what to fix.
type gitCommandError struct {
	command string
	stderr  string
	cause   error
}

func (e *gitCommandError) Error() string {
	message := e.command + ": " + e.cause.Error()
	if e.stderr != "" {
		message += ": " + e.stderr
	}

	return message
}

func (e *gitCommandError) Unwrap() error { return e.cause }

// classifyGit turns a failed git invocation into a classified error. A
// context that was cancelled or expired is reported as exactly that, under
// the family go-error-family assigns to it; anything else takes the family and
// code the caller names, with git's stderr kept in the message.
func classifyGit(
	ctx context.Context,
	err error,
	family errorfamily.Family,
	code, format string,
	args ...any,
) error {
	failure, ok := errors.AsType[*gitCommandError](err)
	if !ok {
		return err
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return errorfamily.Wrapf(ctxErr, errorfamily.Classify(ctxErr), codeGitCancelled,
			"%s did not finish: the scan was interrupted", failure.command)
	}

	detail := ""
	if failure.stderr != "" {
		detail = ": " + failure.stderr
	}

	return errorfamily.Wrapf(failure.cause, family, code, "%s (%s) failed%s",
		fmt.Sprintf(format, args...), failure.command, detail)
}

func runGitWithInput(ctx context.Context, dir, input string, args ...string) ([]byte, error) {
	// #nosec G204 -- the subcommand is a fixed literal set and every argument
	// is repository data, never shell input.
	command := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	command.Env = append(command.Environ(), "LC_ALL=C")

	if input != "" {
		command.Stdin = strings.NewReader(input)
	}

	output, err := command.Output()
	if err != nil {
		return nil, &gitCommandError{
			command: "git " + strings.Join(args, " "),
			stderr:  collapseWhitespace(gitStderr(err)),
			cause:   err,
		}
	}

	return output, nil
}

// gitStderr returns what git printed on stderr for a failed command.
func gitStderr(err error) string {
	exitErr, ok := errors.AsType[*exec.ExitError](err)
	if !ok {
		return ""
	}

	return string(exitErr.Stderr)
}

// collapseWhitespace turns git's possibly multi-line stderr into one line.
func collapseWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
