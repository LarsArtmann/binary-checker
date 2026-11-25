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

// ContentVerdict is the result of inspecting a file's leading bytes.
type ContentVerdict struct {
	// Binary reports whether the sample looks like binary data.
	Binary bool
	// Evidence names the signal that decided the verdict: "nul-byte",
	// "text-ratio", or "" for text files.
	Evidence string
}

// ReadSample reads up to SampleSize bytes from path. A file smaller than the
// sample size yields its whole content.
func ReadSample(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	defer func() { _ = file.Close() }()

	buffer := make([]byte, SampleSize)

	read, err := io.ReadFull(file, buffer)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, err
	}

	return buffer[:read], nil
}

// ClassifyContent decides whether a byte sample is binary.
//
// A NUL byte is a deterministic binary signal (full confidence). Otherwise the
// printable-text ratio decides: fewer than TextRatioThreshold printable bytes
// means binary. The ratio path is a heuristic, so callers should report lower
// confidence for it.
func ClassifyContent(sample []byte) ContentVerdict {
	if len(sample) == 0 {
		return ContentVerdict{}
	}

	if bytes.IndexByte(sample, 0) >= 0 {
		return ContentVerdict{Binary: true, Evidence: "nul-byte"}
	}

	printable := 0

	for offset := 0; offset < len(sample); {
		runeValue, runeSize := utf8.DecodeRune(sample[offset:])
		if runeValue == utf8.RuneError && runeSize == 1 {
			// Invalid UTF-8 byte: count only printable ASCII, matching the
			// way a text file with stray bytes still reads as text.
			if sample[offset] >= 0x20 && sample[offset] <= 0x7e {
				printable++
			}

			offset++

			continue
		}

		if unicode.IsPrint(runeValue) || runeValue == '\t' || runeValue == '\n' || runeValue == '\r' {
			printable += runeSize
		}

		offset += runeSize
	}

	if float64(printable)/float64(len(sample)) < TextRatioThreshold {
		return ContentVerdict{Binary: true, Evidence: "text-ratio"}
	}

	return ContentVerdict{}
}
