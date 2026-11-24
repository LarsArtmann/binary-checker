package binarycheck

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
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
	if !errors.Is(err, ErrConfig) {
		t.Fatalf("error = %v, want ErrConfig", err)
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
	if !errors.Is(err, ErrConfig) {
		t.Fatalf("error = %v, want ErrConfig (typo must not pass silently)", err)
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
	if !errors.Is(err, ErrConfig) {
		t.Fatalf("error = %v, want ErrConfig", err)
	}
}

func TestLoadConfigExplicitPathMustExist(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	_, err := LoadConfig(filepath.Join(root, "missing.json"), root)
	if !errors.Is(err, ErrConfig) {
		t.Fatalf("error = %v, want ErrConfig", err)
	}
}

func TestSuppressionReasonRespectsRuleScope(t *testing.T) {
	t.Parallel()

	config := Config{Ignore: []IgnoreEntry{
		{Path: "fixtures/**", Reason: "binary fixture corpus", Rules: []string{RuleTrackedBinary}},
	}}

	if _, ok := config.SuppressionReason("fixtures/sample.bin", RuleTrackedBinary); !ok {
		t.Fatal("expected BC001 suppression")
	}

	if _, ok := config.SuppressionReason("fixtures/sample.bin", RuleTrackedPattern); ok {
		t.Fatal("BC002 must not be suppressed by a BC001-scoped entry")
	}
}

func TestConfigUnlimitedExemption(t *testing.T) {
	t.Parallel()

	allowed := true
	zero := int64(0)

	config := Config{AllowFonts: &allowed, FontMaxBytes: &zero}
	if config.FontCeiling() != 0 {
		t.Fatalf("FontCeiling = %d, want 0", config.FontCeiling())
	}
}
