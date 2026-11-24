package binarycheck

import (
	"path"
	"strings"
)

// MatchPath reports whether a repository-relative path matches a glob pattern.
//
// Supported syntax:
//
//   - `*` and `?` match within one path segment.
//   - `**` matches zero or more whole path segments.
//   - a pattern ending in `/` matches any path under that directory.
//   - a pattern without `/` matches the basename at any depth.
//   - character classes (`[a-z]`) follow path.Match.
//
// Paths and patterns use forward slashes; leading "./" is ignored.
func MatchPath(pattern, relPath string) bool {
	pattern = normalizePattern(pattern)
	relPath = normalizePattern(relPath)

	if pattern == "" {
		return false
	}

	if before, ok := strings.CutSuffix(pattern, "/"); ok {
		dir := before
		return hasDirPrefix(relPath, dir)
	}

	if !strings.Contains(pattern, "/") {
		return matchSegments(strings.Split(pattern, "/"), strings.Split(path.Base(relPath), "/"))
	}

	return matchSegments(strings.Split(pattern, "/"), strings.Split(relPath, "/"))
}

func normalizePattern(value string) string {
	value = strings.ReplaceAll(value, "\\", "/")

	for strings.HasPrefix(value, "./") {
		value = strings.TrimPrefix(value, "./")
	}

	return strings.TrimPrefix(value, "/")
}

func hasDirPrefix(relPath, dir string) bool {
	if MatchPath(dir, relPath) {
		return true
	}

	return strings.HasPrefix(relPath, dir+"/") || strings.Contains(relPath, "/"+dir+"/")
}

// matchSegments matches pattern segments against path segments with `**`
// support. The dynamic-programming table keeps pathological patterns linear in
// the number of segments.
func matchSegments(patterns, segments []string) bool {
	// dp[i][j] == patterns[:i] matches segments[:j].
	dp := make([][]bool, len(patterns)+1)
	for i := range dp {
		dp[i] = make([]bool, len(segments)+1)
	}

	dp[0][0] = true

	for i := 1; i <= len(patterns); i++ {
		if patterns[i-1] == "**" {
			// `**` can match zero segments, so it inherits the row above.
			dp[i][0] = dp[i-1][0]

			for j := 1; j <= len(segments); j++ {
				dp[i][j] = dp[i-1][j] || dp[i][j-1]
			}

			continue
		}

		for j := 1; j <= len(segments); j++ {
			if !dp[i-1][j-1] {
				continue
			}

			matched, err := path.Match(patterns[i-1], segments[j-1])
			if err == nil && matched {
				dp[i][j] = true
			}
		}
	}

	return dp[len(patterns)][len(segments)]
}
