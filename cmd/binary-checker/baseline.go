package main

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	errorfamily "github.com/larsartmann/go-error-family"
	"github.com/larsartmann/go-finding"

	"github.com/larsartmann/binary-checker/pkg/binarycheck"
)

const baselineVersion = 1

type baselineFile struct {
	Version  int      `json:"version"`
	Tool     string   `json:"tool"`
	Findings []string `json:"findings"`
}

// baseline is the set of finding IDs a repository has already accepted.
type baseline struct {
	ids map[string]struct{}
}

// loadBaseline reads a baseline file. An empty path means "no baseline was
// configured", which is an empty accepted set, never a nil pointer: callers can
// always ask whether a finding is accepted.
//
// The file is the user's input, so a missing, unreadable, or malformed
// baseline is a Rejection, never silently treated as "accepts nothing".
func loadBaseline(path string) (*baseline, error) {
	if path == "" {
		return &baseline{ids: make(map[string]struct{})}, nil
	}

	data, err := os.ReadFile(path) // #nosec G304 -- the path is the user's own --baseline argument.
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
			return nil, errorfamily.WrapRejectionf(err, codeBaselineRead, "read baseline %q", path)
		}

		return nil, errorfamily.WrapInfrastructuref(err, codeBaselineReadFailed, "read baseline %q", path)
	}

	var file baselineFile
	if err := json.Unmarshal(data, &file, json.RejectUnknownMembers(true)); err != nil {
		return nil, errorfamily.WrapRejectionf(err, codeBaselineParse, "parse baseline %q", path)
	}

	if file.Version != baselineVersion {
		return nil, errorfamily.Newf(errorfamily.Rejection, codeBaselineVersion,
			"%q has unsupported version %d (want %d)", path, file.Version, baselineVersion)
	}

	if file.Tool != "" && file.Tool != binarycheck.ToolName {
		return nil, errorfamily.Newf(errorfamily.Rejection, codeBaselineTool,
			"%q belongs to tool %q, not %q", path, file.Tool, binarycheck.ToolName)
	}

	ids := make(map[string]struct{}, len(file.Findings))
	for _, id := range file.Findings {
		ids[id] = struct{}{}
	}

	return &baseline{ids: ids}, nil
}

func (b *baseline) contains(item finding.Finding) bool {
	_, accepted := b.ids[string(item.ID)]

	return accepted
}

func saveBaseline(path string, findings []finding.Finding) error {
	file := baselineFile{
		Version:  baselineVersion,
		Tool:     binarycheck.ToolName,
		Findings: make([]string, 0, len(findings)),
	}

	for _, item := range findings {
		file.Findings = append(file.Findings, string(item.ID))
	}

	data, err := json.Marshal(file, jsontext.WithIndentPrefix(""), jsontext.WithIndent("  "))
	if err != nil {
		return errorfamily.WrapOrchestrationf(err, codeBaselineEncode, "encode baseline %q", path)
	}

	return writeAtomic(path, append(data, '\n'))
}

// writeAtomic replaces path with data via a temp file in the same directory.
//
// The temp file is removed on every failure path; a removal failure is joined
// onto the returned error rather than swallowed, so a leaked temp file never
// disappears silently.
func writeAtomic(path string, data []byte) (err error) {
	dir := filepath.Dir(path)
	if dir == "" {
		dir = "."
	}

	temp, err := os.CreateTemp(dir, ".binary-checker-baseline-*")
	if err != nil {
		return errorfamily.WrapInfrastructuref(err, codeBaselineWrite, "create baseline in %q", dir)
	}

	tempName := temp.Name()

	defer func() {
		removeErr := os.Remove(tempName)
		if removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
			err = errors.Join(err,
				errorfamily.WrapInfrastructuref(removeErr, codeBaselineWrite, "remove temp file %q", tempName))
		}
	}()

	if _, writeErr := temp.Write(data); writeErr != nil {
		return errors.Join(
			errorfamily.WrapInfrastructuref(writeErr, codeBaselineWrite, "write %q", tempName),
			closeTemp(temp, tempName),
		)
	}

	if closeErr := temp.Close(); closeErr != nil {
		return errorfamily.WrapInfrastructuref(closeErr, codeBaselineWrite, "close %q", tempName)
	}

	if renameErr := os.Rename(tempName, path); renameErr != nil {
		return errorfamily.WrapInfrastructuref(renameErr, codeBaselineWrite, "replace %q", path)
	}

	return nil
}

func closeTemp(file *os.File, name string) error {
	if err := file.Close(); err != nil {
		return errorfamily.WrapInfrastructuref(err, codeBaselineWrite, "close %q", name)
	}

	return nil
}
