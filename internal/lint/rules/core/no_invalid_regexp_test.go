package core

import (
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

const noInvalidRegexpFile = "/repository/source/Patterns.ts"

func TestNoInvalidRegexpFires(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		sourceText string
	}{
		// A pattern that cannot parse. Written as a literal the compiler would refuse it; written
		// as a string it reaches runtime and throws there.
		{"an unclosed character class", "export const a = new RegExp('[');\n"},
		{"an unclosed group", "export const a = new RegExp('(');\n"},
		{"a quantifier with nothing to repeat", "export const a = new RegExp('*');\n"},
		{"a backwards range", "export const a = new RegExp('[z-a]');\n"},

		// Called rather than constructed. RegExp is one of the few constructors that works both
		// ways, and a rule listening only for `new` is silent on half the calls anyone writes.
		{"a plain call rather than new", "export const a = RegExp('[');\n"},

		// Flags. An unknown flag is a SyntaxError rather than something the engine ignores.
		{"an unknown flag", "export const a = new RegExp('.', 'z');\n"},
		{"a duplicated flag", "export const a = new RegExp('.', 'ii');\n"},

		// u and v are mutually exclusive, and this is the case the ordering inside the flag check
		// exists for: both are individually valid, so a checker that struck valid flags first and
		// asked no further question would accept the pair.
		{"u and v together", "export const a = new RegExp('.', 'uv');\n"},

		// Valid without u, invalid with it. Pins that the flags are applied to the pattern rather
		// than checked beside it: `\p` is a property escape only under u, and a lone `{` after it
		// is a syntax error there and a literal brace otherwise.
		{"a pattern the u flag invalidates", "export const a = new RegExp('\\\\p{', 'u');\n"},

		// The half of the option decision that a fixture can actually hold.
		//
		// Upstream takes `allowConstructorFlags`, and under `["a"]` this exact line is clean there.
		// This port implements no option surface, so it must sit on upstream's default empty
		// allow-list and report. That choice is invisible to every other case in this file, because
		// they all exercise flags no allow-list would name; this one names the branch.
		//
		// If someone later adds the option and wires its default wrong, this is the case that fails.
		{"a flag only an allow-list could excuse", "export const a = new RegExp('.', 'a');\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			rule_testing.ExpectFindings(t, rule_testing.Run(t, NoInvalidRegexp, noInvalidRegexpFile, testCase.sourceText),
				"invalidRegexp")
		})
	}
}

func TestNoInvalidRegexpStaysSilent(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		sourceText string
	}{
		// The shapes the rule is asking for.
		{"a valid pattern", "export const a = new RegExp('[a-z]+');\n"},
		{"a valid pattern with flags", "export const a = new RegExp('[a-z]+', 'gi');\n"},
		{"every valid flag at once", "export const a = new RegExp('.', 'dgimsuy');\n"},

		// No arguments produces the empty pattern and cannot fail.
		{"no arguments at all", "export const a = new RegExp();\n"},

		// Not a literal, so what it holds at runtime is unknowable. Reporting it would flag correct
		// code, which is worse than missing a defect.
		{"a pattern from a variable", "export const a = new RegExp(source);\n"},
		{"flags from a variable", "export const a = new RegExp('[a-z]', flags);\n"},

		// Unknown flags with a pattern that some reachable flag combination accepts. `\p{L}` is
		// valid under u, so the variable may well supply it, and this must stay silent. It is the
		// counterpart to the u-invalidates case above and the reason that branch is not simply
		// "check without flags".
		{"a u-only pattern with unknown flags", "export const a = new RegExp('\\\\p{L}', flags);\n"},

		// The case that separates "invalid under every reachable flag set" from "invalid under
		// any". `\p{` is a malformed property escape under u and v, and a literal backslash-p
		// followed by a brace without them, so a checker asking "any" would report a pattern that
		// is fine the moment the flags variable holds something other than u or v.
		//
		// Added after mutation testing: weakening the guard from && to || broke nothing, because
		// every silent case here was valid under all three flag sets and never reached the branch.
		// A clean case positioned where the branch cannot be reached passes while measuring nothing.
		{"a pattern only some flag sets reject", "export const a = new RegExp('\\\\p{', flags);\n"},

		// A different identifier that happens to take similar arguments.
		{"a constructor that is not RegExp", "export const a = new Other('[');\n"},
		{"a call that is not RegExp", "export const a = other('[');\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			rule_testing.ExpectClean(t, rule_testing.Run(t, NoInvalidRegexp, noInvalidRegexpFile, testCase.sourceText))
		})
	}
}
