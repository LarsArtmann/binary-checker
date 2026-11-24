package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/larsartmann/go-finding"

	binarycheck "github.com/larsartmann/binary-checker"
)

const baselineVersion = 1

var errBaseline = errors.New("binary-checker baseline")

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
		return nil, fmt.Errorf("%w: read %q: %w", errBaseline, path, err)
	}

	var file baselineFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("%w: parse %q: %w", errBaseline, path, err)
	}

	if file.Version != baselineVersion {
		return nil, fmt.Errorf("%w: %q has unsupported version %d (want %d)", errBaseline, path, file.Version, baselineVersion)
	}

	if file.Tool != "" && file.Tool != binarycheck.ToolName {
		return nil, fmt.Errorf("%w: %q belongs to tool %q, not %q", errBaseline, path, file.Tool, binarycheck.ToolName)
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

	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("%w: encode %q: %w", errBaseline, path, err)
	}

	data = append(data, '\n')

	dir := filepath.Dir(path)
	if dir == "" {
		dir = "."
	}

	temp, err := os.CreateTemp(dir, ".binary-checker-baseline-*")
	if err != nil {
		return fmt.Errorf("%w: create baseline in %q: %w", errBaseline, dir, err)
	}

	tempName := temp.Name()

	cleanup := func() { _ = os.Remove(tempName) }

	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()

		cleanup()

		return fmt.Errorf("%w: write %q: %w", errBaseline, tempName, err)
	}

	if err := temp.Close(); err != nil {
		cleanup()

		return fmt.Errorf("%w: close %q: %w", errBaseline, tempName, err)
	}

	if err := os.Rename(tempName, path); err != nil {
		cleanup()

		return fmt.Errorf("%w: replace %q: %w", errBaseline, path, err)
	}

	return nil
}
