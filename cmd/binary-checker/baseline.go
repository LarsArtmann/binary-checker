package main

import (
	"encoding/json/v2"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"encoding/json/jsontext"

	"github.com/larsartmann/go-finding"
	errorfamily "github.com/larsartmann/go-error-family"

	binarycheck "github.com/larsartmann/binary-checker"
)

const (
	baselineVersion = 1
	codeBaseline    = "binary_checker.baseline"
)

// errBaseline marks baseline problems: the file itself is the defect.
var errBaseline error = errorfamily.NewRejection(codeBaseline, "binary-checker baseline")

type baselineFile struct {
	Version  int      `json:"version"`
	Tool     string   `json:"tool"`
	Findings []string `json:"findings"`
}

type baseline struct {
	ids map[string]bool
}

func loadBaseline(path string) (*baseline, error) {
	if path == "" {
		return nil, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errorfamily.WrapInfrastructuref(err, codeBaseline, "read %q", path)
	}

	var file baselineFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, errorfamily.WrapRejectionf(err, codeBaseline, "parse %q", path)
	}

	if file.Version != baselineVersion {
		return nil, errorfamily.WrapRejectionf(
			errBaseline, codeBaseline,
			"%q has unsupported version %d (want %d)", path, file.Version, baselineVersion,
		)
	}

	if file.Tool != "" && file.Tool != binarycheck.ToolName {
		return nil, errorfamily.WrapRejectionf(
			errBaseline, codeBaseline,
			"%q belongs to tool %q, not %q", path, file.Tool, binarycheck.ToolName,
		)
	}

	ids := make(map[string]bool, len(file.Findings))
	for _, id := range file.Findings {
		ids[id] = true
	}

	return &baseline{ids: ids}, nil
}

func (b *baseline) contains(item finding.Finding) bool {
	return b != nil && b.ids[string(item.ID)]
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
		return errorfamily.WrapInfrastructuref(err, codeBaseline, "encode %q", path)
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
		return errorfamily.WrapInfrastructuref(err, codeBaseline, "create baseline in %q", dir)
	}

	tempName := temp.Name()

	defer func() {
		removeErr := os.Remove(tempName)
		if removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
			err = errorfamily.Compose(
				err,
				errorfamily.WrapInfrastructuref(removeErr, codeBaseline, "remove temp file %q", tempName),
			)
		}
	}()

	if _, writeErr := temp.Write(data); writeErr != nil {
		return errorfamily.Compose(
			errorfamily.WrapInfrastructuref(writeErr, codeBaseline, "write %q", tempName),
			closeTemp(temp, tempName),
		)
	}

	if closeErr := temp.Close(); closeErr != nil {
		return errorfamily.WrapInfrastructuref(closeErr, codeBaseline, "close %q", tempName)
	}

	if renameErr := os.Rename(tempName, path); renameErr != nil {
		return errorfamily.WrapInfrastructuref(renameErr, codeBaseline, "replace %q", path)
	}

	return nil
}

func closeTemp(file *os.File, name string) error {
	if err := file.Close(); err != nil {
		return errorfamily.WrapInfrastructuref(err, codeBaseline, "close %q", name)
	}

	return nil
}
