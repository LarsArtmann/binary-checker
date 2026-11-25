package binarycheck

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
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

// IgnoreEntry suppresses findings for matching paths. A non-empty reason is
// mandatory: an unexplained ignore is debt that outlives its justification.
type IgnoreEntry struct {
	// Path is a glob matched against repository-relative paths (see MatchPath).
	Path string `json:"path"`
	// Reason explains why the path is deliberately allowed.
	Reason string `json:"reason"`
	// Rules limits the suppression to the listed rule IDs. Empty suppresses
	// every rule.
	Rules []RuleID `json:"rules,omitempty"`
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
//
// Every failure is a Rejection with the offending file named: the config file
// is the defect, not the repository contents.
func LoadConfig(explicitPath, root string) (Config, error) {
	path, err := resolveConfigPath(explicitPath, root)
	if err != nil {
		return Config{}, err
	}

	config, err := readConfigFile(path, explicitPath != "")
	if err != nil {
		return Config{}, err
	}

	if err := config.validate(path); err != nil {
		return Config{}, err
	}

	return config, nil
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
		return "", errorfamily.WrapRejectionf(err, codeConfigPath, "resolve config path %q", explicitPath)
	}

	return absolute, nil
}

// readConfigFile decodes the config at path. A missing file yields the
// defaults unless the caller named it explicitly, in which case it MUST exist.
func readConfigFile(path string, required bool) (Config, error) {
	file, err := os.Open(path) // #nosec G304 -- the path is the scanned repository's config, by design.
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && !required {
			return DefaultConfig(), nil
		}

		return Config{}, errorfamily.WrapRejectionf(err, codeConfigOpen, "open config %q", path)
	}

	defer func() { _ = file.Close() }()

	decoder := jsontext.NewDecoder(file)

	var config Config
	if err := json.UnmarshalDecode(decoder, &config, json.RejectUnknownMembers(true)); err != nil {
		return Config{}, errorfamily.WrapRejectionf(err, codeConfigParse, "parse config %q", path)
	}

	// Reject trailing JSON values: a second object is almost always an
	// accidental copy-paste, and silently ignoring it hides the mistake.
	if _, err := decoder.ReadValue(); !errors.Is(err, io.EOF) {
		return Config{}, errorfamily.Newf(errorfamily.Rejection, codeConfigTrailingData,
			"parse config %q: trailing data after the JSON object", path)
	}

	return config, nil
}

// Validate checks the configuration for actionable mistakes.
func (c Config) Validate() error {
	return c.validate("config")
}

// validate is Validate with the configuration's source named in every
// message, so the user is told which file to edit.
func (c Config) validate(source string) error {
	knownRules := make(map[RuleID]bool, len(ruleTable))
	for _, id := range AllRuleIDs() {
		knownRules[id] = true
	}

	for index, entry := range c.Ignore {
		if strings.TrimSpace(entry.Path) == "" {
			return errorfamily.Newf(errorfamily.Rejection, codeConfigInvalid,
				"%s: ignore[%d] has an empty path", source, index)
		}

		if strings.TrimSpace(entry.Reason) == "" {
			return errorfamily.Newf(errorfamily.Rejection, codeConfigInvalid,
				"%s: ignore[%d] (%s) must state a reason: an unexplained ignore outlives its justification",
				source, index, entry.Path)
		}

		for _, rule := range entry.Rules {
			if !knownRules[rule] {
				return errorfamily.Newf(errorfamily.Rejection, codeConfigInvalid,
					"%s: ignore[%d] (%s) names unknown rule %q (known: %s)",
					source, index, entry.Path, rule, JoinRuleIDs(AllRuleIDs()))
			}
		}
	}

	if c.FontMaxBytes != nil && *c.FontMaxBytes < 0 {
		return errorfamily.Newf(errorfamily.Rejection, codeConfigInvalid,
			"%s: font_max_bytes must be >= 0", source)
	}

	if c.SmallImageMaxBytes != nil && *c.SmallImageMaxBytes < 0 {
		return errorfamily.Newf(errorfamily.Rejection, codeConfigInvalid,
			"%s: small_image_max_bytes must be >= 0", source)
	}

	return nil
}

// SuppressionReason returns the reason of the first ignore entry matching
// relPath for ruleID.
func (c Config) SuppressionReason(relPath string, ruleID RuleID) (string, bool) {
	for _, entry := range c.Ignore {
		if !MatchPath(entry.Path, relPath) {
			continue
		}

		if len(entry.Rules) > 0 && !slices.Contains(entry.Rules, ruleID) {
			continue
		}

		return entry.Reason, true
	}

	return "", false
}
