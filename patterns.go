package binarycheck

import (
	"path"
	"strings"
)

// binaryArtifactPatterns lists path shapes that denote build artifacts,
// executables, archives, or media. Patterns are lowercase and matched against
// the lowercased repository-relative path (see MatchPath).
var binaryArtifactPatterns = []string{
	// Executables and shared objects.
	"*.exe", "*.dll", "*.dylib", "*.so", "*.so.*", "*.kexe", "*.out",
	"*.app/", "__pycache__/", "node_modules/", ".gradle/",

	// JVM artifacts.
	"*.class", "*.jar", "*.war", "*.ear", "*.nar",

	// Compiled objects and static libraries.
	"*.o", "*.a", "*.lib", "*.obj", "*.pyc", "*.pyo",

	// Archives and disk images.
	"*.zip", "*.tar", "*.tar.gz", "*.tgz", "*.tar.xz", "*.txz",
	"*.tar.bz2", "*.tbz2", "*.gz", "*.bz2", "*.xz", "*.zst",
	"*.7z", "*.rar", "*.iso", "*.img",

	// Other opaque build outputs.
	"*.bin",

	// Images that are commonly committed assets (exempt under a size limit
	// unless the user disables the exemption).
	"*.png", "*.jpg", "*.jpeg", "*.gif", "*.bmp", "*.ico", "*.webp",
	"*.tif", "*.tiff", "*.psd", "*.ai",

	// Web fonts (exempt under a size limit unless the user disables it).
	"*.ttf", "*.otf", "*.woff", "*.woff2", "*.eot",

	// Audio and video.
	"*.mp3", "*.wav", "*.flac", "*.ogg", "*.m4a", "*.aac",
	"*.mp4", "*.avi", "*.mkv", "*.wmv", "*.flv", "*.mov", "*.webm",

	// Binary documents.
	"*.pdf", "*.doc", "*.docx", "*.xls", "*.xlsx", "*.ppt", "*.pptx",
	"*.odt",

	// Databases.
	"*.db", "*.sqlite", "*.sqlite3",

	// Operating-system noise.
	".ds_store",
}

// FontPatterns are web-font extensions. They are legitimate repository assets
// under the configured size limit.
var FontPatterns = []string{"*.ttf", "*.otf", "*.woff", "*.woff2", "*.eot"}

// SmallImagePatterns are image extensions that are legitimate repository
// assets under the configured size limit. SVG is deliberately absent: it is
// text, not binary.
var SmallImagePatterns = []string{"*.ico", "*.png", "*.jpg", "*.jpeg", "*.gif", "*.webp"}

// MatchBinaryArtifact returns the first binary-artifact pattern matching
// relPath, or "" when no pattern matches.
func MatchBinaryArtifact(relPath string) string {
	lower := strings.ToLower(strings.ReplaceAll(relPath, "\\", "/"))

	for _, pattern := range binaryArtifactPatterns {
		if MatchPath(pattern, lower) {
			return pattern
		}
	}

	return ""
}

// IsFont reports whether relPath has a web-font extension.
func IsFont(relPath string) bool {
	return matchesAnyExtension(relPath, FontPatterns)
}

// IsSmallImage reports whether relPath has an image extension eligible for the
// small-image exemption.
func IsSmallImage(relPath string) bool {
	return matchesAnyExtension(relPath, SmallImagePatterns)
}

func matchesAnyExtension(relPath string, patterns []string) bool {
	base := strings.ToLower(path.Base(strings.ReplaceAll(relPath, "\\", "/")))

	for _, pattern := range patterns {
		if matched, err := path.Match(pattern, base); err == nil && matched {
			return true
		}
	}

	return false
}
