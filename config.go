package binarycheck

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	errorfamily "github.com/larsartmann/go-error-family"
)

const (
	// DefaultConfigName is the config file auto-discovered at the git root.
	DefaultConfigName = ".binary-checker.json"

	// DefaultFontMaxBytes is the default exemption ceiling for web fonts.
	DefaultFontMaxBytes int64 = 512 * 1024

	// DefaultSmallImageMaxBytes is the default exemption ceiling for small
	// image assets (favicons, icons, sprites).
	DefaultSmallImageMaxBytes int64 = 1024 * 1024
)

const codeConfig = "binary_checker.config"

// ErrConfig marks configuration problems: the input file is the defect, not
// the repository contents. It is a Rejection: retrying cannot help.
var ErrConfig error = errorfamily.NewRejection(codeConfig, "binary-checker config")

// IgnoreEntry suppresses findings for matching paths. A non-empty reason is
// mandatory: an unexplained ignore is debt that outlives its justification.
type IgnoreEntry struct {
	// Path is a glob matched against repository-relative paths (see MatchPath).
	Path string `json:"path"`
	// Reason explains why the path is deliberately allowed.
	Reason string `json:"reason"`
	// Rules limits the suppression to the listed rule IDs. Empty suppresses
	// every rule.
	Rules []string `json:"rules,omitempty"`
}

// Config is the on-disk configuration schema. Missing fields take documented
// defaults; unknown fields are rejected so a typo cannot silently do nothing.
type Config struct {
	Ignore             []IgnoreEntry `json:"ignore,omitempty"`
	AllowFonts         *bool         `json:"allow_fonts,omitempty"`
	FontMaxBytes       *int64        `json:"font_max_bytes,omitempty"`
	AllowSmallImages   *bool         `json:"allow_small_images,omitempty"`
	SmallImageMaxBytes *int64        `json:"small_image_max_bytes,omitempty"`
}

// DefaultConfig returns the configuration used when no file is present.
func DefaultConfig() Config {
	return Config{}
}

// AllowFontsEnabled reports whether the web-font exemption is active.
func (c Config) AllowFontsEnabled() bool {
	return c.AllowFonts == nil || *c.AllowFonts
}

// FontCeiling returns the effective web-font size limit.
func (c Config) FontCeiling() int64 {
	if c.FontMaxBytes == nil {
		return DefaultFontMaxBytes
	}

	return *c.FontMaxBytes
}

// AllowSmallImagesEnabled reports whether the small-image exemption is active.
func (c Config) AllowSmallImagesEnabled() bool {
	return c.AllowSmallImages == nil || *c.AllowSmallImages
}

// SmallImageCeiling returns the effective small-image size limit.
func (c Config) SmallImageCeiling() int64 {
	if c.SmallImageMaxBytes == nil {
		return DefaultSmallImageMaxBytes
	}

	return *c.SmallImageMaxBytes
}

// LoadConfig loads configuration for a repository.
//
// explicitPath, when non-empty, is resolved relative to the process working
// directory and MUST exist. Otherwise DefaultConfigName is looked up at the git
// root; absence yields defaults, not an error.
func LoadConfig(explicitPath, root string) (Config, error) {
	path, err := resolveConfigPath(explicitPath, root)
	if err != nil {
		return Config{}, err
	}

	return loadConfigFile(path, explicitPath != "")
}

// resolveConfigPath turns the --config flag (or its absence) into an absolute
// path. An empty explicitPath means the default name at the git root.
func resolveConfigPath(explicitPath, root string) (string, error) {
	if explicitPath == "" {
		return filepath.Join(root, DefaultConfigName), nil
	}

	if filepath.IsAbs(explicitPath) {
		return explicitPath, nil
	}

	absolute, err := filepath.Abs(explicitPath)
	if err != nil {
		return "", errorfamily.WrapRejectionf(
			err, codeConfig, "resolve explicit config path %q", explicitPath,
		)
	}

	return absolute, nil
}

func loadConfigFile(path string, required bool) (Config, error) {
	config, err := readConfigFile(path, required)
	if err != nil {
		return Config{}, errorfamily.WrapRejectionf(err, codeConfig, "read config %q", path)
	}

	if err := config.Validate(); err != nil {
		return Config{}, errorfamily.WrapRejectionf(err, codeConfig, "validate config %q", path)
	}

	return config, nil
}

func readConfigFile(path string, required bool) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && !required {
			return DefaultConfig(), nil
		}

		return Config{}, errorfamily.WrapRejectionf(err, codeConfig, "open %q", path)
	}

	defer func() { _ = file.Close() }()

	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()

	var config Config
	if err := decoder.Decode(&config); err != nil {
		return Config{}, errorfamily.WrapRejectionf(err, codeConfig, "parse %q", path)
	}

	// Reject trailing JSON values: a second object is almost always an
	// accidental copy-paste, and silently ignoring it hides the mistake.
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Config{}, errorfamily.Newf(errorfamily.Rejection, 
			codeConfig, "parse %q: trailing data after the JSON object", path,
		)
	}

	return config, nil
}

// Validate checks the configuration for actionable mistakes.
func (c Config) Validate() error {
	knownRules := make(map[string]bool)

	for _, id := range AllRuleIDs() {
		knownRules[id] = true
	}

	for i, entry := range c.Ignore {
		if strings.TrimSpace(entry.Path) == "" {
			return errorfamily.Newf(errorfamily.Rejection, codeConfig, "ignore[%d] has an empty path", i)
		}

		if strings.TrimSpace(entry.Reason) == "" {
			return errorfamily.Newf(errorfamily.Rejection, 
				codeConfig,
				"ignore[%d] (%s) must state a reason: an unexplained ignore outlives its justification",
				i,
				entry.Path,
			)
		}

		for _, rule := range entry.Rules {
			if !knownRules[rule] {
				return errorfamily.Newf(errorfamily.Rejection, 
					codeConfig, "ignore[%d] (%s) names unknown rule %q", i, entry.Path, rule,
				)
			}
		}
	}

	if c.FontMaxBytes != nil && *c.FontMaxBytes < 0 {
		return errorfamily.Newf(errorfamily.Rejection, codeConfig, "font_max_bytes must be >= 0")
	}

	if c.SmallImageMaxBytes != nil && *c.SmallImageMaxBytes < 0 {
		return errorfamily.Newf(errorfamily.Rejection, codeConfig, "small_image_max_bytes must be >= 0")
	}

	return nil
}

// SuppressionReason returns the reason of the first ignore entry matching
// relPath for ruleID.
func (c Config) SuppressionReason(relPath, ruleID string) (string, bool) {
	for _, entry := range c.Ignore {
		if !MatchPath(entry.Path, relPath) {
			continue
		}

		if len(entry.Rules) > 0 && !contains(entry.Rules, ruleID) {
			continue
		}

		return entry.Reason, true
	}

	return "", false
}

func contains(values []string, target string) bool {
	return slices.Contains(values, target)
}
