package binarycheck

import (
	"context"
	"path/filepath"
	"runtime"
	"sync"

	errorfamily "github.com/larsartmann/go-error-family"
)

// inspectionDisposition says how far the inspection of one tracked file got.
// The dispositions are exclusive by construction, so a file can never be, say,
// exempt and skipped at the same time.
type inspectionDisposition uint8

const (
	// dispositionInspected: the content was read and classified.
	dispositionInspected inspectionDisposition = iota
	// dispositionExempt: a web font or small image allowed by policy.
	dispositionExempt
	// dispositionLFS: a Git LFS pointer; the blob lives outside git.
	dispositionLFS
	// dispositionSkipped: nothing was inspected, because the context ended or
	// the file is absent from the working tree or unreadable.
	dispositionSkipped
)

// fileInspection is what the checker learned about one tracked file.
type fileInspection struct {
	file        trackedFile
	disposition inspectionDisposition
	size        int64
	verdict     ContentVerdict
	pattern     string
}

// ruleForInspection returns the rule an inspection offends against, or nil
// for a file that offends against none. Only inspected content can: an
// exempt, LFS, or skipped file was never judged.
func ruleForInspection(inspection fileInspection) *ruleSpec {
	switch {
	case inspection.disposition != dispositionInspected:
		return nil
	case inspection.verdict.Binary:
		return ruleTrackedBinary
	case inspection.pattern != "":
		return ruleTrackedPattern
	default:
		return nil
	}
}

// inspectAll inspects every tracked file with a bounded worker pool. It fails
// when the context ends first: the files it never looked at must not come
// back as "skipped" inside an otherwise successful result.
func inspectAll(
	ctx context.Context,
	root string,
	files []trackedFile,
	config Config,
	lfs map[string]bool,
) ([]fileInspection, error) {
	workers := max(min(runtime.NumCPU(), maxScanWorkers), 1)

	inspections := make([]fileInspection, len(files))
	jobs := make(chan int)

	var waitGroup sync.WaitGroup
	for range workers {
		waitGroup.Go(func() {
			for index := range jobs {
				inspections[index] = inspectFile(ctx, root, files[index], config, lfs)
			}
		})
	}

	for index := range files {
		if ctx.Err() != nil {
			break
		}

		jobs <- index
	}

	close(jobs)
	waitGroup.Wait()

	if err := ctx.Err(); err != nil {
		return nil, errorfamily.Wrapf(err, errorfamily.Classify(err), codeScanInterrupted,
			"inspect tracked files under %q", root)
	}

	return inspections, nil
}

// inspectFile measures one tracked file. An exempt asset is released before
// its content is read: its size alone decides, so the read is wasted work.
func inspectFile(
	ctx context.Context,
	root string,
	file trackedFile,
	config Config,
	lfs map[string]bool,
) fileInspection {
	inspection := fileInspection{file: file}

	if ctx.Err() != nil {
		inspection.disposition = dispositionSkipped

		return inspection
	}

	if lfs[file.Path] {
		inspection.disposition = dispositionLFS

		return inspection
	}

	sample, size, err := ReadSample(filepath.Join(root, filepath.FromSlash(file.Path)))
	if err != nil {
		inspection.disposition = dispositionSkipped

		return inspection
	}

	inspection.size = size
	inspection.pattern = MatchBinaryArtifact(file.Path)

	if isExemptAsset(file.Path, size, config) {
		inspection.disposition = dispositionExempt

		return inspection
	}

	inspection.verdict = ClassifyContent(sample)

	return inspection
}

// isExemptAsset reports whether relPath is a web font or small image within
// its configured size ceiling.
func isExemptAsset(relPath string, size int64, config Config) bool {
	if IsFont(relPath) && config.AllowFontsEnabled() && size <= config.FontCeiling() {
		return true
	}

	return IsSmallImage(relPath) && config.AllowSmallImagesEnabled() && size <= config.SmallImageCeiling()
}
