package binarycheck

import (
	"bytes"
	"errors"
	"io"
	"os"
	"unicode"
	"unicode/utf8"
)

const (
	// SampleSize is the number of leading bytes inspected for binary content.
	// Mirrors git's own buffer heuristic: 8000 bytes is enough to catch every
	// common binary format without reading whole artifacts.
	SampleSize = 8000

	// TextRatioThreshold is the minimum ratio of printable bytes for a sample
	// to be considered text. Below it, the file is binary.
	TextRatioThreshold = 0.7
)

// Evidence names the signal that decided a content verdict. The zero value
// means text: no signal fired.
type Evidence string

const (
	// EvidenceNulByte reports a NUL byte in the sample. Deterministic.
	EvidenceNulByte Evidence = "nul-byte"
	// EvidenceTextRatio reports too few printable bytes. A heuristic.
	EvidenceTextRatio Evidence = "text-ratio"
)

// ContentVerdict is the result of inspecting a file's leading bytes.
type ContentVerdict struct {
	// Binary reports whether the sample looks like binary data.
	Binary bool
	// Evidence names the signal that decided a binary verdict.
	Evidence Evidence
}

// ReadSample opens path, confirms it is a regular file, and reads up to
// SampleSize leading bytes. It returns the sample and the file's size; a file
// smaller than the sample size yields its whole content.
//
// The size is taken from the descriptor that is read, not from a separate
// stat of the path, so nothing can be swapped in between. A path that is not
// a regular file fails with an error matching ErrNotRegularFile.
func ReadSample(path string) ([]byte, int64, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, 0, err
	}

	if !info.Mode().IsRegular() {
		return nil, 0, ErrNotRegularFile
	}

	// #nosec G304 -- the path comes from the git index of the scanned
	// repository; reading its leading bytes is this tool's whole purpose.
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}

	defer func() { _ = file.Close() }()

	opened, err := file.Stat()
	if err != nil {
		return nil, 0, err
	}

	if !opened.Mode().IsRegular() {
		return nil, 0, ErrNotRegularFile
	}

	buffer := make([]byte, SampleSize)

	read, err := io.ReadFull(file, buffer)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, 0, err
	}

	return buffer[:read], opened.Size(), nil
}

// ClassifyContent decides whether a byte sample is binary.
//
// A NUL byte is a deterministic binary signal. Otherwise the printable-text
// ratio decides: fewer than TextRatioThreshold printable bytes means binary.
// The ratio path is a heuristic, so callers report lower confidence for it.
func ClassifyContent(sample []byte) ContentVerdict {
	if len(sample) == 0 {
		return ContentVerdict{}
	}

	if bytes.IndexByte(sample, 0) >= 0 {
		return ContentVerdict{Binary: true, Evidence: EvidenceNulByte}
	}

	printable := 0

	for offset := 0; offset < len(sample); {
		runeValue, runeSize := utf8.DecodeRune(sample[offset:])
		if runeValue == utf8.RuneError && runeSize == 1 {
			// An invalid UTF-8 byte is never printable; skip it.
			offset++

			continue
		}

		if unicode.IsPrint(runeValue) || runeValue == '\t' || runeValue == '\n' || runeValue == '\r' {
			printable += runeSize
		}

		offset += runeSize
	}

	if float64(printable)/float64(len(sample)) < TextRatioThreshold {
		return ContentVerdict{Binary: true, Evidence: EvidenceTextRatio}
	}

	return ContentVerdict{}
}
