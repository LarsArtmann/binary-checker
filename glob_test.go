package binarycheck

import "testing"

func TestMatchPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		pattern string
		path    string
		want    bool
	}{
		{pattern: "*.bin", path: "blob.bin", want: true},
		{pattern: "*.bin", path: "nested/blob.bin", want: true},
		{pattern: "*.bin", path: "blob.bin.bak", want: false},
		{pattern: "assets/*.png", path: "assets/logo.png", want: true},
		{pattern: "assets/*.png", path: "assets/icons/logo.png", want: false},
		{pattern: "assets/**", path: "assets/icons/logo.png", want: true},
		{pattern: "**/*.psd", path: "design/source/file.psd", want: true},
		{pattern: "**/*.psd", path: "file.psd", want: true},
		{pattern: "testdata/**", path: "testdata/nested/fixture.bin", want: true},
		{pattern: "testdata/", path: "testdata/nested/fixture.bin", want: true},
		{pattern: "node_modules/", path: "packages/app/node_modules/x/index.js", want: true},
		{pattern: "node_modules/", path: "node_modules", want: true},
		{pattern: "./blob.bin", path: "blob.bin", want: true},
		{pattern: "**", path: "any/deep/path.txt", want: true},
		{pattern: "a/**/z", path: "a/z", want: true},
		{pattern: "a/**/z", path: "a/b/c/z", want: true},
		{pattern: "a/**/z", path: "a/b/c/y", want: false},
		{pattern: "", path: "blob.bin", want: false},
	}

	for _, test := range tests {
		t.Run(test.pattern+"_"+test.path, func(t *testing.T) {
			t.Parallel()

			if got := MatchPath(test.pattern, test.path); got != test.want {
				t.Fatalf("MatchPath(%q, %q) = %v, want %v", test.pattern, test.path, got, test.want)
			}
		})
	}
}
