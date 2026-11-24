package binarycheck

import "testing"

func TestClassifyContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		sample   []byte
		binary   bool
		evidence string
	}{
		{name: "empty", sample: nil, binary: false},
		{name: "ascii text", sample: []byte("hello, world\n"), binary: false},
		{name: "utf8 text", sample: []byte("héllo wörld — 日本語\n"), binary: false},
		{name: "nul byte", sample: []byte{'a', 0, 'b'}, binary: true, evidence: "nul-byte"},
		{name: "png header", sample: []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0}, binary: true, evidence: "nul-byte"},
		{
			name:     "control heavy",
			sample:   []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 'a', 'b'},
			binary:   true,
			evidence: "text-ratio",
		},
		{
			name:   "mostly text with stray control bytes",
			sample: []byte("this is a long text file with one stray \x01 byte in the middle of prose\n"),
			binary: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			verdict := ClassifyContent(test.sample)
			if verdict.Binary != test.binary {
				t.Fatalf("Binary = %v, want %v (evidence %q)", verdict.Binary, test.binary, verdict.Evidence)
			}

			if verdict.Evidence != test.evidence {
				t.Fatalf("Evidence = %q, want %q", verdict.Evidence, test.evidence)
			}
		})
	}
}
