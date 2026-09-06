package core

import (
	"testing"

	"github.com/system-inc/cohere/internal/rule"
	"github.com/system-inc/cohere/internal/rule_testing"
	"github.com/system-inc/cohere/internal/utilities/ecmascript/regexsyntax"
)

// backreferenceFile is where the fixtures pretend to live.
const backreferenceFile = "/repository/source/Backreference.ts"

// The corpus is oxc's, copied rather than rewritten.
//
// Every case below is verbatim from
// `oxc/crates/oxc_linter/src/rules/eslint/no_useless_backreference.rs`: 99 pass, 88 fail. The
// snapshot records 93 diagnostics from those 88 fail inputs, so one finding per input is wrong
// here. Five inputs report twice and each carries two ids below; the counts were recovered by
// aligning every snapshot diagnostic on the source line it prints, which is sound for this rule
// because all 88 inputs are distinct as text and the alignment came out a bijection.
//
// The ids are per problem kind rather than one shared id, because upstream emits five different
// sentences and the kind is the diagnosis. A single id would let a port that calls a
// different-alternative case a forward reference pass every fixture here.
func TestNoUselessBackreferenceFires(t *testing.T) {
	t.Parallel()

	cases := []struct {
		sourceText string
		findings   []string
	}{
		{`/(b)(\2a)/`, []string{"backreferenceInsideItsOwnGroup"}},
		{`/\k<foo>(?<foo>bar)/`, []string{"backreferenceBeforeItsGroup"}},
		{`RegExp('(a|bc)|\\1')`, []string{"backreferenceToAnotherAlternative"}},
		{`new RegExp('(?!(?<foo>\\n))\\1')`, []string{"backreferenceIntoNegativeLookaround"}},
		{`/(?<!(a)\1)b/`, []string{"backreferenceAfterItsGroupInLookbehind"}},
		{`new RegExp('(\\1)')`, []string{"backreferenceInsideItsOwnGroup"}},
		{`/^(a\1)$/`, []string{"backreferenceInsideItsOwnGroup"}},
		{`/^((a)\1)$/`, []string{"backreferenceInsideItsOwnGroup"}},
		{`new RegExp('^(a\\1b)$')`, []string{"backreferenceInsideItsOwnGroup"}},
		{`RegExp('^((\\1))$')`, []string{"backreferenceInsideItsOwnGroup"}},
		{`/((\2))/`, []string{"backreferenceInsideItsOwnGroup"}},
		{`/a(?<foo>(.)b\1)/`, []string{"backreferenceInsideItsOwnGroup"}},
		{`/a(?<foo>\k<foo>)b/`, []string{"backreferenceInsideItsOwnGroup"}},
		{`/^(\1)*$/`, []string{"backreferenceInsideItsOwnGroup"}},
		{`/^(?:a)(?:((?:\1)))*$/`, []string{"backreferenceInsideItsOwnGroup"}},
		{`/(?!(\1))/`, []string{"backreferenceInsideItsOwnGroup"}},
		{`/a|(b\1c)/`, []string{"backreferenceInsideItsOwnGroup"}},
		{`/(a|(\1))/`, []string{"backreferenceInsideItsOwnGroup"}},
		{`/(a|(\2))/`, []string{"backreferenceInsideItsOwnGroup"}},
		{`/(?:a|(\1))/`, []string{"backreferenceInsideItsOwnGroup"}},
		{`/(a)?(b)*(\3)/`, []string{"backreferenceInsideItsOwnGroup"}},
		{`/(?<=(a\1))b/`, []string{"backreferenceInsideItsOwnGroup"}},
		{`/\1(a)/`, []string{"backreferenceBeforeItsGroup"}},
		{`/\1.(a)/`, []string{"backreferenceBeforeItsGroup"}},
		{`/(?:\1)(?:(a))/`, []string{"backreferenceBeforeItsGroup"}},
		{`/(?:\1)(?:((a)))/`, []string{"backreferenceBeforeItsGroup"}},
		{`/(?:\2)(?:((a)))/`, []string{"backreferenceBeforeItsGroup"}},
		{`/(?:\1)(?:((?:a)))/`, []string{"backreferenceBeforeItsGroup"}},
		{`/(\2)(a)/`, []string{"backreferenceBeforeItsGroup"}},
		{`RegExp('(a)\\2(b)')`, []string{"backreferenceBeforeItsGroup"}},
		{`/(?:a)(b)\2(c)/`, []string{"backreferenceBeforeItsGroup"}},
		{`/\k<foo>(?<foo>a)/`, []string{"backreferenceBeforeItsGroup"}},
		{`/(?:a(b)\2)(c)/`, []string{"backreferenceBeforeItsGroup"}},
		{`new RegExp('(a)(b)\\3(c)')`, []string{"backreferenceBeforeItsGroup"}},
		{`/\1(?<=(a))./`, []string{"backreferenceBeforeItsGroup"}},
		{`/\1(?<!(a))./`, []string{"backreferenceBeforeItsGroup"}},
		{`/(?<=\1)(?<=(a))/`, []string{"backreferenceBeforeItsGroup"}},
		{`/(?<!\1)(?<!(a))/`, []string{"backreferenceBeforeItsGroup"}},
		{`/(?=\1(a))./`, []string{"backreferenceBeforeItsGroup"}},
		{`/(?!\1(a))./`, []string{"backreferenceBeforeItsGroup"}},
		{`/(?<=(a)\1)b/`, []string{"backreferenceAfterItsGroupInLookbehind"}},
		{`/(?<!.(a).\1.)b/`, []string{"backreferenceAfterItsGroupInLookbehind"}},
		{`/(.)(?<!(b|c)\2)d/`, []string{"backreferenceAfterItsGroupInLookbehind"}},
		{`/(?<=(?:(a)\1))b/`, []string{"backreferenceAfterItsGroupInLookbehind"}},
		{`/(?<=(?:(a))\1)b/`, []string{"backreferenceAfterItsGroupInLookbehind"}},
		{`/(?<=(a)(?:\1))b/`, []string{"backreferenceAfterItsGroupInLookbehind"}},
		{`/(?<!(?:(a))(?:\1))b/`, []string{"backreferenceAfterItsGroupInLookbehind"}},
		{`/(?<!(?:(a))(?:\1)|.)b/`, []string{"backreferenceAfterItsGroupInLookbehind"}},
		{`/.(?!(?<!(a)\1))./`, []string{"backreferenceAfterItsGroupInLookbehind"}},
		{`/.(?=(?<!(a)\1))./`, []string{"backreferenceAfterItsGroupInLookbehind"}},
		{`/.(?!(?<=(a)\1))./`, []string{"backreferenceAfterItsGroupInLookbehind"}},
		{`/.(?=(?<=(a)\1))./`, []string{"backreferenceAfterItsGroupInLookbehind"}},
		{`/(a)|\1b/`, []string{"backreferenceToAnotherAlternative"}},
		{`/^(?:(a)|\1b)$/`, []string{"backreferenceToAnotherAlternative"}},
		{`/^(?:(a)|b(?:c|\1))$/`, []string{"backreferenceToAnotherAlternative"}},
		{`/^(?:a|b(?:(c)|\1))$/`, []string{"backreferenceToAnotherAlternative"}},
		{`/^(?:(a(?!b))|\1b)+$/`, []string{"backreferenceToAnotherAlternative"}},
		{`/^(?:(?:(a)(?!b))|\1b)+$/`, []string{"backreferenceToAnotherAlternative"}},
		{`/^(?:(a(?=a))|\1b)+$/`, []string{"backreferenceToAnotherAlternative"}},
		{`/^(?:(a)(?=a)|\1b)+$/`, []string{"backreferenceToAnotherAlternative"}},
		{`/.(?:a|(b)).|(?:(\1)|c)./`, []string{"backreferenceToAnotherAlternative"}},
		{`/.(?!(a)|\1)./`, []string{"backreferenceToAnotherAlternative"}},
		{`/.(?<=\1|(a))./`, []string{"backreferenceToAnotherAlternative"}},
		{`/a(?!(b)).\1/`, []string{"backreferenceIntoNegativeLookaround"}},
		{`/(?<!(a))b\1/`, []string{"backreferenceIntoNegativeLookaround"}},
		{`/(?<!(a))(?:\1)/`, []string{"backreferenceIntoNegativeLookaround"}},
		{`/.(?<!a|(b)).\1/`, []string{"backreferenceIntoNegativeLookaround"}},
		{`/.(?!(a)).(?!\1)./`, []string{"backreferenceIntoNegativeLookaround"}},
		{`/.(?<!(a)).(?<!\1)./`, []string{"backreferenceIntoNegativeLookaround"}},
		{`/.(?=(?!(a))\1)./`, []string{"backreferenceIntoNegativeLookaround"}},
		{`/.(?<!\1(?!(a)))/`, []string{"backreferenceIntoNegativeLookaround"}},
		{`/\1(a)(b)\2/`, []string{"backreferenceBeforeItsGroup"}},
		{`/\1(a)\1/`, []string{"backreferenceBeforeItsGroup"}},
		{`/\1(a)\2(b)/`, []string{"backreferenceBeforeItsGroup", "backreferenceBeforeItsGroup"}},
		{`/\1.(?<=(a)\1)/`, []string{"backreferenceBeforeItsGroup", "backreferenceAfterItsGroupInLookbehind"}},
		{`/(?!\1(a)).\1/`, []string{"backreferenceBeforeItsGroup", "backreferenceIntoNegativeLookaround"}},
		{`/(a)\2(b)/; RegExp('(\\1)');`, []string{"backreferenceBeforeItsGroup", "backreferenceInsideItsOwnGroup"}},
		{`RegExp('\\1(a){', flags);`, []string{"backreferenceBeforeItsGroup"}},
		{`new RegExp('\\1([[A--B]])', 'v')`, []string{"backreferenceBeforeItsGroup"}},
		{`/\k<foo>((?<foo>bar)|(?<foo>baz))/`, []string{"backreferenceBeforeItsGroup"}},
		{`/((?<foo>bar)|\k<foo>(?<foo>baz))/`, []string{"backreferenceToAnotherAlternative"}},
		{`/\k<foo>((?<foo>bar)|(?<foo>baz)|(?<foo>qux))/`, []string{"backreferenceBeforeItsGroup"}},
		{`/((?<foo>bar)|\k<foo>(?<foo>baz)|(?<foo>qux))/`, []string{"backreferenceToAnotherAlternative"}},
		{`/((?<foo>bar)|\k<foo>|(?<foo>baz))/`, []string{"backreferenceToAnotherAlternative"}},
		{`/((?<foo>bar)|\k<foo>|(?<foo>baz)|(?<foo>qux))/`, []string{"backreferenceToAnotherAlternative"}},
		{`/((?<foo>bar)|(?<foo>baz\k<foo>)|(?<foo>qux\k<foo>))/`, []string{"backreferenceToAnotherAlternative", "backreferenceToAnotherAlternative"}},
		{`/(?<=((?<foo>bar)|(?<foo>baz))\k<foo>)/`, []string{"backreferenceAfterItsGroupInLookbehind"}},
		{`/((?!(?<foo>bar))|(?!(?<foo>baz)))\k<foo>/`, []string{"backreferenceIntoNegativeLookaround"}},
	}

	for _, testCase := range cases {
		t.Run(testCase.sourceText, func(t *testing.T) {
			rule_testing.ExpectFindings(t,
				rule_testing.Run(t, NoUselessBackreference, backreferenceFile, testCase.sourceText),
				testCase.findings...)
		})
	}
}

// The clean cases are the whole discrimination and they are what catches a port.
//
// Several groups are worth naming because each encodes a distinction that is easy to miss:
// a callee that is not the global RegExp (`regExp`, `Regexp`, `RegExp.foo`, `foo.RegExp`); a
// pattern the rule cannot read (an identifier, a concatenation, a template with substitutions);
// an escape that is not a backreference at all (`\0`, `\11` with one group, a `\1` inside a
// character class, a doubled backslash); a backreference that is genuinely reachable, including
// the many lookaround shapes where the group does run before the reference; and patterns whose
// syntax is broken, which upstream skips because its parser refuses them.
func TestNoUselessBackreferenceStaysSilent(t *testing.T) {
	t.Parallel()

	cases := []struct {
		sourceText string
	}{
		{`'\1(a)'`},
		{`regExp('\\1(a)')`},
		{`new Regexp('\\1(a)', 'u')`},
		{`RegExp.foo('\\1(a)', 'u')`},
		{`new foo.RegExp('\\1(a)')`},
		{`RegExp(p)`},
		{`new RegExp(p, 'u')`},
		{`RegExp('\\1(a)' + suffix)`},
		{"new RegExp(`${prefix}\\\\1(a)`)"},
		{`/(?:)/`},
		{`/(?:a)/`},
		{`new RegExp('')`},
		{`RegExp('(?:a)|(?:b)*')`},
		{`/^ab|[cd].\n$/`},
		{`/(a)/`},
		{`RegExp('(a)|(b)')`},
		{`new RegExp('\\n\\d(a)')`},
		{`/\0(a)/`},
		{`/\0(a)/u`},
		{`/(?<=(a))(b)(?=(c))/`},
		{`/(?<!(a))(b)(?!(c))/`},
		{`/(?<foo>a)/`},
		{`RegExp('\1(a)')`},
		{`RegExp('\\\\1(a)')`},
		{`/\\1(a)/`},
		{`/\1/`},
		{`/^\1$/`},
		{`/\2(a)/`},
		{`/\1(?:a)/`},
		{`/\1(?=a)/`},
		{`/\1(?!a)/`},
		{`/^[\1](a)$/`},
		{`new RegExp('[\\1](a)')`},
		{`/\11(a)/`},
		{`/\k<foo>(a)/`},
		{`/^(a)\1\\2$/`},
		{`/(a)\1/`},
		{`/(a).\1/`},
		{`RegExp('(a)\\1(b)')`},
		{`/(a)(b)\2(c)/`},
		{`/(?<foo>a)\k<foo>/`},
		{`new RegExp('(.)\\1')`},
		{`RegExp('(a)\\1(?:b)')`},
		{`/(a)b\1/`},
		{`/((a)\2)/`},
		{`/((a)b\2c)/`},
		{`/^(?:(a)\1)$/`},
		{`/^((a)\2)$/`},
		{`/^(((a)\3))|b$/`},
		{`/a(?<foo>(.)b\2)/`},
		{`/(a)?(b)*(\1)(c)/`},
		{`/(a)?(b)*(\2)(c)/`},
		{`/(?<=(a))b\1/`},
		{`/(?<=(?=(a)\1))b/`},
		{`/(?<!\1(a))b/`},
		{`/(?<=\1(a))b/`},
		{`/(?<!\1.(a))b/`},
		{`/(?<=\1.(a))b/`},
		{`/(?<=(?:\1.(a)))b/`},
		{`/(?<!(?:\1)((a)))b/`},
		{`/(?<!(?:\2)((a)))b/`},
		{`/(?=(?<=\1(a)))b/`},
		{`/(?=(?<!\1(a)))b/`},
		{`/(.)(?<=\2(a))b/`},
		{`/^(a)\1|b/`},
		{`/^a|(b)\1/`},
		{`/^a|(b|c)\1/`},
		{`/^(a)|(b)\2/`},
		{`/^(?:(a)|(b)\2)$/`},
		{`/^a|(?:.|(b)\1)/`},
		{`/^a|(?:.|(b).(\1))/`},
		{`/^a|(?:.|(?:(b)).(\1))/`},
		{`/^a|(?:.|(?:(b)|c).(\1))/`},
		{`/^a|(?:.|(?:(b)).(\1|c))/`},
		{`/^a|(?:.|(?:(b)|c).(\1|d))/`},
		{`/.(?=(b))\1/`},
		{`/.(?<=(b))\1/`},
		{`/a(?!(b)\1)./`},
		{`/a(?<!\1(b))./`},
		{`/a(?!(b)(\1))./`},
		{`/a(?!(?:(b)\1))./`},
		{`/a(?!(?:(b))\1)./`},
		{`/a(?<!(?:\1)(b))./`},
		{`/a(?<!(?:(?:\1)(b)))./`},
		{`/(?<!(a))(b)(?!(c))\2/`},
		{`/a(?!(b|c)\1)./`},
		{`RegExp('\\1(a)[')`},
		{`new RegExp('\\1(a){', 'u')`},
		{`new RegExp('\\1(a)\\2', 'ug')`},
		{`RegExp('\\1(a)\\k<foo>', 'u')`},
		{`new RegExp('\\k<foo>(?<foo>a)\\k<bar>')`},
		{`new RegExp('([[A--B]])\\1', 'v')`},
		{`new RegExp('[[]\\1](a)', 'v')`},
		{`/((?<foo>bar)\k<foo>|(?<foo>baz))/`},
	}

	for _, testCase := range cases {
		t.Run(testCase.sourceText, func(t *testing.T) {
			rule_testing.ExpectClean(t,
				rule_testing.Run(t, NoUselessBackreference, backreferenceFile, testCase.sourceText))
		})
	}
}

// TestNoUselessBackreferenceStillReportsBesideALegacyOctal covers a case upstream's corpus does
// not reach.
//
// Written for a surviving mutant. The check that a numeric reference past the last group only
// invalidates the pattern under `u` or `v` could be dropped entirely and every one of the 187
// upstream cases still passed, because none of them puts a useless backreference and an
// out-of-range escape in the same flagless pattern. Without the flag `\2` here is a legacy octal
// and the pattern is valid (confirmed against node), so the useless `\1` must still report; with
// the flag the same text is a syntax error and the whole pattern is upstream's business, not this
// rule's.
func TestNoUselessBackreferenceStillReportsBesideALegacyOctal(t *testing.T) {
	t.Parallel()

	result := rule_testing.Run(t, NoUselessBackreference, backreferenceFile, `/\1(a)\2/`)
	rule_testing.ExpectFindings(t, result, "backreferenceBeforeItsGroup")

	// The same text under `u` is a syntax error, so the rule steps aside entirely.
	rule_testing.ExpectClean(t,
		rule_testing.Run(t, NoUselessBackreference, backreferenceFile, `/\1(a)\2/u`))
}

// TestNoUselessBackreferenceHandlesConstructorArgumentShapesUpstreamNeverExercises covers two
// branches the corpus leaves untested.
//
// Both were written for surviving mutants and both are real behaviors rather than defensive code.
//
// A regex literal handed to the constructor is checked once, by the call, because the call's flags
// argument replaces the literal's own. Dropping the guard that makes the literal listener step
// aside reports the same pattern twice, and no upstream case passes a regex literal to `RegExp` at
// all.
//
// A flags argument that is a template with substitutions cannot be read, and the substitution may
// supply the `u` that decides whether the pattern is even valid, so the call is skipped. Upstream
// draws the same line and distinguishes it from a plain identifier: `RegExp('\\1(a){', flags)` is
// one of its fail cases, because an unreadable identifier is treated as no flags and the pattern
// parses without `u`. Only the template form returns early.
func TestNoUselessBackreferenceHandlesConstructorArgumentShapesUpstreamNeverExercises(t *testing.T) {
	t.Parallel()

	t.Run("a regex literal argument reports once, not twice", func(t *testing.T) {
		rule_testing.ExpectFindings(t,
			rule_testing.Run(t, NoUselessBackreference, backreferenceFile, `RegExp(/\1(a)/, '')`),
			"backreferenceBeforeItsGroup")
	})

	t.Run("an unreadable template flags argument skips the call", func(t *testing.T) {
		rule_testing.ExpectClean(t,
			rule_testing.Run(t, NoUselessBackreference, backreferenceFile,
				"RegExp('\\\\1(a)', `${flags}`)"))
	})

	// The contrast that makes the line above a real distinction rather than a blanket skip: an
	// identifier is unreadable too, and upstream still checks the pattern with no flags.
	t.Run("an identifier flags argument still checks the pattern", func(t *testing.T) {
		rule_testing.ExpectFindings(t,
			rule_testing.Run(t, NoUselessBackreference, backreferenceFile, `RegExp('\\1(a)', flags)`),
			"backreferenceBeforeItsGroup")
	})
}

// TestNoUselessBackreferenceReportsAShadowedRegExp pins the one place this port disagrees with
// upstream, so the disagreement is a decision on the record rather than a gap.
//
// Upstream consults the scope and declines a `RegExp` that is locally bound, which is why these
// five inputs are in its pass list. Answering that is name resolution and needs the checker, and
// `no-new-native-nonconstructor` is the shipped rule establishing there is no structural answer for
// this shape: the shadow can be a parameter, a `var`, a `const` or an import in any enclosing
// scope. This rule does not take the checker, so it reports. If a later change makes it silent
// here, that is upstream parity and this test should be deleted rather than worked around.
func TestNoUselessBackreferenceReportsAShadowedRegExp(t *testing.T) {
	t.Parallel()

	cases := []string{
		`let RegExp; new RegExp('\\1(a)');`,
		`function foo() { var RegExp; RegExp('\\1(a)', 'u'); }`,
		"function foo() { var RegExp; RegExp('\\\\1(a)', `u`); }",
		`function foo(RegExp) { new RegExp('\\1(a)'); }`,
		`if (foo) { const RegExp = bar; RegExp('\\1(a)'); }`,
	}
	for _, sourceText := range cases {
		t.Run(sourceText, func(t *testing.T) {
			rule_testing.ExpectFindings(t,
				rule_testing.Run(t, NoUselessBackreference, backreferenceFile, sourceText),
				"backreferenceBeforeItsGroup")
		})
	}
}

// TestNoUselessBackreferencePointsAtTheBackreference asserts where each finding lands, which
// ExpectFindings cannot see.
//
// This rule carries no repair, and that is exactly why the spans need asserting: a rule whose only
// output is a location passes a complete id fixture while pointing anywhere at all. The constructor
// rows are the ones that matter. The pattern the rule scans is the literal's cooked value and the
// file holds the raw text, so `'(a)\\2(b)'` is eleven bytes on disk and eight cooked. Every
// constructor case in the corpus went silent while this was wrong, but a pattern differing only in
// escape width would have reported at a plausible-looking wrong offset instead, and no id fixture
// would have noticed.
func TestNoUselessBackreferencePointsAtTheBackreference(t *testing.T) {
	t.Parallel()

	cases := []struct {
		sourceText string
		reported   []string
	}{
		{`/\1(a)/`, []string{`\1`}},
		{`/(b)(\2a)/`, []string{`\2`}},
		{`/(a|\1b)/`, []string{`\1`}},
		{`/\1(?!(a))/`, []string{`\1`}},
		// A lookbehind matches right to left, so the reference is the one that comes late here.
		{`/(?<=(a)\1)b/`, []string{`\1`}},
		{`/\k<foo>(?<foo>bar)/`, []string{`\k<foo>`}},
		// Two findings in one pattern, so the second span cannot be right by accident.
		{`/\1(a)\2(b)/`, []string{`\1`, `\2`}},
		// The constructor rows: raw and cooked differ, so an offset taken from the cooked pattern
		// lands mid-literal.
		{`RegExp('(a)\\2(b)')`, []string{`\\2`}},
		{`new RegExp('(\\1)')`, []string{`\\1`}},
		{`RegExp('(a|bc)|\\1')`, []string{`\\1`}},
		{`new RegExp('(?!(?<foo>\\n))\\1')`, []string{`\\1`}},
		{`new RegExp('\\1([[A--B]])', 'v')`, []string{`\\1`}},
	}

	for _, testCase := range cases {
		t.Run(testCase.sourceText, func(t *testing.T) {
			result := rule_testing.Run(t, NoUselessBackreference, backreferenceFile, testCase.sourceText)
			if len(result.Diagnostics) != len(testCase.reported) {
				t.Fatalf("expected %d findings, got %d", len(testCase.reported), len(result.Diagnostics))
			}
			for index, want := range testCase.reported {
				diagnostic := result.Diagnostics[index]
				got := testCase.sourceText[diagnostic.Range.Pos():diagnostic.Range.End()]
				if got != want {
					t.Errorf("finding %d points at %q, expected %q", index, got, want)
				}
			}
		})
	}
}

// TestNoUselessBackreferenceEveryPathStartsAtTheRoot pins the invariant that lets the
// lowest-common-ancestor walk start at index 1.
//
// Written for a surviving mutant. Starting that walk at 0 instead of 1 survived the whole corpus,
// and reading the code says why: the root node is pushed before the scan loop and never popped
// inside it, so every recorded path begins with index 0 and the comparison at 0 is always 0 == 0.
// The mutant is therefore equivalent, and the only input that could distinguish the two is a path
// not beginning at the root, which the scanner cannot produce. This asserts that rather than
// leaving it as an argument, so a later change that pushes something before the root fails here
// instead of silently changing what the walk means.
// Every path recorded by the scanner must begin with the root node, index 0. That invariant is
// what makes starting the lowest-common-ancestor walk at 1 rather than 0 the same computation:
// the comparison at 0 is 0 == 0 for every pair of paths, so it can only ever advance.
func TestNoUselessBackreferenceEveryPathStartsAtTheRoot(t *testing.T) {
	t.Parallel()

	patterns := []string{
		`\1(a)`, `(b)(\2a)`, `(a|\1b)`, `\1(?!(a))`, `(?<=(a)\1)b`, `\k<foo>(?<foo>bar)`,
		`((?<foo>bar)|\k<foo>(?<foo>baz)|(?<foo>qux))`, `(?<=((?<foo>bar)|(?<foo>baz))\k<foo>)`,
		`.(?=(?!(a))\1).`, `^(?:(a(?!b))|\1b)+$`, `\1([[A--B]])`, `[\1](a)`,
	}
	for _, pattern := range patterns {
		for _, flagText := range []string{"", "u", "v"} {
			structure, ok := scanRegexStructure(pattern, regexsyntax.ParseRegexFlags(flagText))
			if !ok {
				continue
			}
			for _, group := range structure.groups {
				if len(group.path) == 0 || group.path[0] != 0 {
					t.Errorf("group path %v in %q flags %q does not start at the root", group.path, pattern, flagText)
				}
			}
			for _, reference := range structure.backreferences {
				if len(reference.path) == 0 || reference.path[0] != 0 {
					t.Errorf("reference path %v in %q flags %q does not start at the root", reference.path, pattern, flagText)
				}
			}
		}
	}
}

// TestNoUselessBackreferenceStaysSilentOnAPatternTheEngineWouldRefuse covers the scanner's error
// paths, which the corpus reaches only glancingly.
//
// Written for three surviving mutants: dropping the balanced-parenthesis check, accepting a `{`
// with no digits as a quantifier, and ignoring a failed escape skip. All three are reachable and
// none is defensive, because a pattern arrives here as a *string* and a string can hold anything.
// `RegExp('\\1(a')` is a runtime error the engine raises, not a lint finding, and a scanner that
// walks it anyway builds a group list whose spans are nonsense and then reports against them.
//
// Upstream reaches two of these (`RegExp('\\1(a)[')` is in its pass vector) and stops there, so
// the unbalanced and trailing-backslash shapes are ours.
func TestNoUselessBackreferenceStaysSilentOnAPatternTheEngineWouldRefuse(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		sourceText string
	}{
		{"an unclosed group", `RegExp('\\1(a')`},
		{"an unopened group", `RegExp('\\1(a))')`},
		{"a trailing backslash", `RegExp('(a)\\1\\')`},
		{"an unterminated class", `RegExp('(a)\\1[')`},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			rule_testing.ExpectClean(t,
				rule_testing.Run(t, NoUselessBackreference, backreferenceFile, testCase.sourceText))
		})
	}

	// A brace that opens no quantifier is a literal brace without `u`, so the pattern is valid and
	// the useless backreference in it still reports. This is the contrast that makes the digit
	// check in opensQuantifier a real decision rather than an unreachable guard: read it as a
	// quantifier and the pattern is judged well-formed under `u` when it is not.
	t.Run("a literal brace does not suppress the finding", func(t *testing.T) {
		rule_testing.ExpectFindings(t,
			rule_testing.Run(t, NoUselessBackreference, backreferenceFile, `RegExp('\\1(a){,}')`),
			"backreferenceBeforeItsGroup")
	})
	t.Run("the same brace under u is a syntax error", func(t *testing.T) {
		rule_testing.ExpectClean(t,
			rule_testing.Run(t, NoUselessBackreference, backreferenceFile, `RegExp('\\1(a){,}', 'u')`))
	})
}

// The registry entry, so a rule that passes every fixture and lints zero files fails here.
//
// The NeedsTypeChecker assertion is the negative on purpose. This rule is purely syntactic: it
// reads pattern text and the shape of the call around it, and never asks what a name binds to.
// Declaring the checker would take an exclusive per-file lock for nothing. The one question that
// would need it, whether a `RegExp` callee is locally shadowed, is the stated divergence above.
func TestNoUselessBackreferenceIsRegistered(t *testing.T) {
	t.Parallel()

	var found *rule.Registration
	for index, registration := range rule.Registered() {
		if registration.Rule.Name == "no-useless-backreference" {
			found = &rule.Registered()[index]
			break
		}
	}
	if found == nil {
		t.Fatal("no-useless-backreference is absent from the registry")
	}
	if found.Rule.NeedsTypeChecker {
		t.Error("the registered copy declares NeedsTypeChecker; this rule is purely syntactic")
	}
}
