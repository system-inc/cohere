package configuration

import "testing"

// TestKeyReachesRuleRunsOneWay pins the direction the resolver and the orphaned-key report share. The
// report once tested the reverse, so a bare key against a prefixed registry name read as resolving
// while the resolver left the rule unconfigured (#7ya3xwe follow-up, 2026-10-02).
func TestKeyReachesRuleRunsOneWay(t *testing.T) {
	cases := []struct {
		key, ruleName string
		want          bool
	}{
		{"no-console", "no-console", true},
		{"nexus/consistency-no-enum", "consistency-no-enum", true},
		{"@typescript-eslint/no-unused-vars", "@typescript-eslint/no-unused-vars", true},
		{"no-unused-vars", "@typescript-eslint/no-unused-vars", false},
		{"no-enum", "consistency-no-enum", false},
		{"typescript/no-base-to-string", "@typescript-eslint/no-base-to-string", false},
	}
	for _, testCase := range cases {
		if got := KeyReachesRule(testCase.key, testCase.ruleName); got != testCase.want {
			t.Errorf("KeyReachesRule(%q, %q) = %v, want %v", testCase.key, testCase.ruleName, got, testCase.want)
		}
	}

	// The resolver must agree with the predicate on the case that caused the incident.
	config := &Config{Rules: map[string]RuleSetting{"no-unused-vars": {Severity: SeverityError}}}
	if status, _ := config.Resolve("a.ts").StatusOf("@typescript-eslint/no-unused-vars"); status != StatusUnconfigured {
		t.Errorf("a bare key configured the prefixed rule, status %v", status)
	}
}
