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

// lsStageFields is the number of whitespace-separated fields `git ls-files
// --stage` emits before the tab-delimited path: mode, object id, and stage.
const lsStageFields = 3

// Index entry modes that carry no file content to inspect.
const (
	indexModeSymlink   = "120000"
	indexModeSubmodule = "160000"
)

// indexStageMerged is the stage of an ordinary index entry. Higher stages
// belong to a merge in progress and repeat a path already listed at stage 0.
const indexStageMerged = "0"

// trackedFile is one stage-0 file entry from the git index.
type trackedFile struct {
	// Path is repository-relative and slash-separated.
	Path string
}

// gitClient wraps read-only git queries rooted at a work tree.
type gitClient struct {
	root string
}

// gitCondition names why a git query matters. It supplies the code and family
// an error gets when git fails to answer that query; the caller knows which
// condition a failure represents, the runner does not.
type gitCondition struct {
	code   string
	family errorfamily.Family
}

// gitNoWorkTree: the target is not inside a git work tree.
var gitNoWorkTree = gitCondition{ //nolint:gochecknoglobals // a fixed condition descriptor; it holds no state
	code: codeGitNotRepository, family: errorfamily.Rejection,
}

// gitQueryFailed: git could not answer a query it normally answers.
var gitQueryFailed = gitCondition{ //nolint:gochecknoglobals // a fixed condition descriptor; it holds no state
	code: codeGitFailed, family: errorfamily.Infrastructure,
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
		return "", classifyGit(ctx, err, gitNoWorkTree,
			"not a git repository (or any parent up to %q)", dir)
	}

	root := strings.TrimSpace(string(output))
	if root == "" {
		return "", errorfamily.Newf(errorfamily.Rejection, codeGitNotRepository,
			"not a git repository (or any parent up to %q)", dir)
	}

	return filepath.Clean(root), nil
}

// tracked returns every stage-0 index entry that carries file content, and
// how many entries carry none (symlinks and submodules).
func (g *gitClient) tracked(ctx context.Context) ([]trackedFile, int, error) {
	output, err := runGit(ctx, g.root, "ls-files", "--stage", "-z")
	if err != nil {
		return nil, 0, classifyGit(ctx, err, gitQueryFailed, "list tracked files in %q", g.root)
	}

	var files []trackedFile

	contentLess := 0

	seen := make(map[string]struct{})

	for record := range bytes.SplitSeq(output, []byte{0}) {
		before, after, ok := bytes.Cut(record, []byte{'\t'})
		if !ok {
			continue
		}

		fields := strings.Fields(string(before))
		if len(fields) < lsStageFields {
			continue
		}

		mode, stage, path := fields[0], fields[2], string(after)
		if stage != indexStageMerged {
			continue
		}

		if _, duplicate := seen[path]; duplicate {
			continue
		}

		seen[path] = struct{}{}

		switch mode {
		case indexModeSymlink, indexModeSubmodule:
			contentLess++
		default:
			files = append(files, trackedFile{Path: path})
		}
	}

	return files, contentLess, nil
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
		return nil, classifyGit(ctx, err, gitQueryFailed,
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
// A failed lookup fails the scan: continuing without LFS knowledge would
// report LFS pointers as binary, and the user would never learn why.
func (g *gitClient) lfsPaths(ctx context.Context, paths []string) (map[string]bool, error) {
	result := make(map[string]bool)
	if len(paths) == 0 {
		return result, nil
	}

	input := strings.Join(paths, "\x00") + "\x00"

	output, err := runGitWithInput(ctx, g.root, input, "check-attr", "-z", "--stdin", "filter")
	if err != nil {
		return nil, classifyGit(ctx, err, gitQueryFailed,
			"look up Git LFS attributes in %q", g.root)
	}

	fields := bytes.Split(output, []byte{0})

	for index := 0; index+2 < len(fields); index += 3 {
		path := string(fields[index])
		if string(fields[index+2]) == "lfs" {
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

// classifyGit turns a failed git invocation into a classified error.
//
// A context that ended is reported as exactly that, under the family
// go-error-family assigns to it: the caller stopped the scan, git did not
// fail. A missing git executable is an environment problem. Anything else
// takes the caller's condition, with git's stderr kept in the message.
func classifyGit(
	ctx context.Context,
	err error,
	condition gitCondition,
	format string,
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

	code, family := condition.code, condition.family
	if errors.Is(err, exec.ErrNotFound) {
		code, family = codeGitMissing, errorfamily.Infrastructure
	}

	return errorfamily.Wrapf(failure.cause, family, code, "%s (%s) failed%s",
		fmt.Sprintf(format, args...), failure.command, stderrDetail(failure.stderr))
}

func stderrDetail(stderr string) string {
	if stderr == "" {
		return ""
	}

	return ": " + stderr
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
