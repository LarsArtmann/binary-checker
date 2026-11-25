package binarycheck

import (
	"os"
	"path/filepath"
	"testing"

	errorfamily "github.com/larsartmann/go-error-family"
)

func TestLoadConfigDefaultsWhenAbsent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	config, err := LoadConfig("", root)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if !config.AllowFontsEnabled() || config.FontCeiling() != DefaultFontMaxBytes {
		t.Fatalf("font defaults wrong: %+v", config)
	}

	if !config.AllowSmallImagesEnabled() || config.SmallImageCeiling() != DefaultSmallImageMaxBytes {
		t.Fatalf("small-image defaults wrong: %+v", config)
	}
}

func TestLoadConfigRejectsUnexplainedIgnore(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, DefaultConfigName)

	content := `{"ignore":[{"path":"testdata/**","reason":"  "}]}`

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig("", root)
	if errorfamily.Code(err) != codeConfigInvalid {
		t.Fatalf("code = %q, want %q (error: %v)", errorfamily.Code(err), codeConfigInvalid, err)
	}
}

func TestLoadConfigRejectsUnknownField(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, DefaultConfigName)

	content := `{"ignore":[],"ignroe":[]}`

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig("", root)
	if errorfamily.Code(err) != codeConfigParse {
		t.Fatalf("code = %q, want %q: a typo must not pass silently (error: %v)",
			errorfamily.Code(err), codeConfigParse, err)
	}
}

func TestLoadConfigRejectsUnknownRule(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, DefaultConfigName)

	content := `{"ignore":[{"path":"x","reason":"fixture","rules":["BC999"]}]}`

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig("", root)
	if errorfamily.Code(err) != codeConfigInvalid {
		t.Fatalf("code = %q, want %q (error: %v)", errorfamily.Code(err), codeConfigInvalid, err)
	}
}

func TestLoadConfigExplicitPathMustExist(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	_, err := LoadConfig(filepath.Join(root, "missing.json"), root)
	if errorfamily.Code(err) != codeConfigOpen {
		t.Fatalf("code = %q, want %q (error: %v)", errorfamily.Code(err), codeConfigOpen, err)
	}
}

func TestSuppressionReasonRespectsRuleScope(t *testing.T) {
	t.Parallel()

	config := Config{Ignore: []IgnoreEntry{
		{Path: "fixtures/**", Reason: "binary fixture corpus", Rules: []RuleID{RuleTrackedBinary}},
	}}

	if _, ok := config.SuppressionReason("fixtures/sample.bin", RuleTrackedBinary); !ok {
		t.Fatal("expected BC001 suppression")
	}

	if _, ok := config.SuppressionReason("fixtures/sample.bin", RuleTrackedPattern); ok {
		t.Fatal("BC002 must not be suppressed by a BC001-scoped entry")
	}
}

func TestConfigExplicitZeroCeilingIsRespected(t *testing.T) {
	t.Parallel()

	allowed := true
	zero := int64(0)

	config := Config{AllowFonts: &allowed, FontMaxBytes: &zero}
	if config.FontCeiling() != 0 {
		t.Fatalf("FontCeiling = %d, want 0", config.FontCeiling())
	}
}
