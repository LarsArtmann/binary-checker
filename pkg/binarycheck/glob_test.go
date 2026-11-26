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

// A pattern ending in "/" promises "any path under that directory". The
// directory segment may itself contain globs, so matching only literal
// prefixes silently breaks the documented contract.
func TestMatchPathDirectoryPatternsWithGlobbedSegments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		pattern string
		path    string
		want    bool
	}{
		{pattern: "*.app/", path: "Foo.app/Contents/Info.plist", want: true},
		{pattern: "*.app/", path: "dist/Foo.app/Contents/MacOS/tool", want: true},
		{pattern: "*.app/", path: "Foo.app", want: true},
		{pattern: "*.app/", path: "Foo.appx/Contents/Info.plist", want: false},
		{pattern: "*.app/", path: "notes.app-notes/Info.plist", want: false},
		{pattern: "build-*/", path: "src/build-linux/out.o", want: true},
		{pattern: "build-*/", path: "src/build-linux", want: true},
		{pattern: "build-*/", path: "src/build/out.o", want: false},
		{pattern: "**/fixtures/", path: "a/b/fixtures/x.bin", want: true},
		{pattern: "**/fixtures/", path: "fixtures/x.bin", want: true},
		{pattern: "**/fixtures/", path: "a/fixtures-backup/x.bin", want: false},
		{pattern: "pkgs/*/assets/", path: "pkgs/web/assets/logo.png", want: true},
		{pattern: "pkgs/*/assets/", path: "pkgs/web/src/logo.png", want: false},
		{pattern: "a/**/", path: "a/b/c/d.txt", want: true},
		{pattern: "a/**/", path: "a", want: true},
		{pattern: "a/**/", path: "ab/c.txt", want: false},
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
