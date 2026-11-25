package binarycheck

import (
	"context"
	"testing"

	"github.com/larsartmann/go-finding"
	linter "github.com/larsartmann/go-linter-sdk"

	"github.com/larsartmann/binary-checker/internal/gittest"
)

func TestRuleIDsAreStable(t *testing.T) {
	t.Parallel()

	want := []string{"BC001", "BC002", "BC003"}
	got := AllRuleIDs()

	if len(got) != len(want) {
		t.Fatalf("rule IDs = %v, want %v", got, want)
	}

	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("rule IDs = %v, want %v", got, want)
		}
	}
}

func TestDefaultRegistryExcludesOptInRule(t *testing.T) {
	t.Parallel()

	dir := gittest.NewRepo(t)
	gittest.WriteFile(t, dir, ".gitignore", []byte("*.log\n"))
	gittest.WriteFile(t, dir, "debug.log", []byte("debug\n"))
	gittest.AddFiles(t, dir, ".gitignore", "debug.log")

	registry := NewRegistry(NewChecker())

	if registry.Has(RuleTrackedIgnored) {
		t.Fatal("BC003 is opt-in and must not be registered by default")
	}

	report, err := registry.Run(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(report.FindByRule(finding.RuleName(RuleTrackedIgnored))) != 0 {
		t.Fatal("default registry must not emit BC003 findings")
	}
}

func TestOptInRuleRunsWhenSelected(t *testing.T) {
	t.Parallel()

	dir := gittest.NewRepo(t)
	gittest.WriteFile(t, dir, ".gitignore", []byte("*.log\n"))
	gittest.WriteFile(t, dir, "debug.log", []byte("debug\n"))
	gittest.AddFiles(t, dir, ".gitignore", "debug.log")

	checker := NewChecker()
	all := Rules(checker)

	if !hasRule(all, RuleTrackedIgnored) {
		t.Fatal("Rules() must include the opt-in rule")
	}

	registry := linter.NewRegistryFromRules(all, linter.WithToolName(ToolName))

	report, err := registry.Run(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(report.FindByRule(finding.RuleName(RuleTrackedIgnored))) != 1 {
		t.Fatal("expected one BC003 finding when the opt-in rule is registered")
	}
}

func hasRule(rules []linter.Rule, id string) bool {
	for _, rule := range rules {
		if rule.ID() == id {
			return true
		}
	}

	return false
}
