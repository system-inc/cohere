package typescript

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/testing"
)

const banTsCommentFile = "/repository/source/Thing.ts"

// decodeBanTsCommentOptions runs a configuration through the real decoder rather than building
// the options struct directly.
//
// The decoder holds five defaults, a polymorphic value for four of the keys, and a regex
// compile, so a fixture constructing BanTsCommentOptions by hand would leave the whole config
// half of the port unexercised. An empty configuration is the bare `"error"` case, which the
// config layer turns into no options, and it reaches the rule the same way here.
func decodeBanTsCommentOptions(test *testing.T, configuration string) rule.Given[BanTsCommentOptions] {
	test.Helper()

	if configuration == "" {
		// A rule configured as a bare `"error"` is handed no options, not an empty struct. Handing
		// it none here is what puts the rule's own fallback under test rather than the decoder's.
		return rule.Given[BanTsCommentOptions]{}
	}
	options, decodeBanTsCommentOptionsError := DecodeBanTsCommentOptions([]byte(configuration))
	if decodeBanTsCommentOptionsError != nil {
		test.Fatalf("could not decode %s: %v", configuration, decodeBanTsCommentOptionsError)
	}
	return rule.Configured(options)
}

// TestBanTsCommentUpstreamCorpus replays typescript-eslint 8.71.0's own rows and the edge rows
// (ban_ts_comment_corpus_data_test.go says how they were recorded). Each row asserts the message id,
// the exact text the finding covers, and what its suggestion writes, and that no finding carries an
// autofix, because upstream offers none.
func TestBanTsCommentUpstreamCorpus(test *testing.T) {
	test.Parallel()

	valid, invalid := 0, 0
	for _, row := range banTsCommentCorpus {
		if row.edge != "" {
			continue
		}
		if len(row.findings) == 0 {
			valid++
		} else {
			invalid++
		}
	}
	if valid != banTsCommentCorpusUpstreamValid || invalid != banTsCommentCorpusUpstreamInvalid {
		test.Fatalf("the corpus holds %d valid and %d invalid upstream rows, and upstream has %d and %d",
			valid, invalid, banTsCommentCorpusUpstreamValid, banTsCommentCorpusUpstreamInvalid)
	}

	for _, row := range banTsCommentCorpus {
		name := fmt.Sprintf("upstream-%d", row.index)
		if row.edge != "" {
			name = row.edge
		}
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunGiven(test, BanTsComment, banTsCommentFile, row.source,
				decodeBanTsCommentOptions(test, row.options))
			if len(row.findings) == 0 {
				rule_testing.ExpectClean(test, result)
				return
			}

			diagnostics := result.Diagnostics
			sort.SliceStable(diagnostics, func(first, second int) bool {
				return diagnostics[first].Range.Pos() < diagnostics[second].Range.Pos()
			})
			reported := []banTsCommentCorpusFinding{}
			for _, diagnostic := range diagnostics {
				if len(diagnostic.Fixes) > 0 {
					test.Errorf("the rule proposes an autofix typescript-eslint 8.71.0 does not: %+v", diagnostic.Fixes)
				}
				suggestion := ""
				if len(diagnostic.Suggestions) > 0 {
					for _, fix := range diagnostic.Suggestions[0].Fixes {
						suggestion = row.source[:fix.Range.Pos()] + fix.Text + row.source[fix.Range.End():]
					}
				}
				reported = append(reported, banTsCommentCorpusFinding{
					id:         diagnostic.Message.Id,
					text:       row.source[diagnostic.Range.Pos():diagnostic.Range.End()],
					suggestion: suggestion,
				})
			}
			if fmt.Sprint(reported) != fmt.Sprint(row.findings) {
				test.Fatalf("the rule reports %q, and typescript-eslint 8.71.0 reports %q", reported, row.findings)
			}
			// Through the harness assertion as well, so the docs capture records the case.
			ids := []string{}
			for _, finding := range row.findings {
				ids = append(ids, finding.id)
			}
			rule_testing.ExpectFindings(test, result, ids...)
		})
	}
}

// banTsCommentCaseName names a subtest by its index in a table.
func banTsCommentCaseName(index int) string {
	return "case-" + strconv.Itoa(index)
}

// TestBanTsCommentDefaultsBindWithNoConfiguration is the fixture that bypasses the decoder.
//
// A rule configured as a bare `"error"` is handed nil options, and a bare type assertion on nil
// yields four zero-valued settings that match no arm, which registers the rule on every file and
// reports nothing while every decoder-routed fixture stays green. This passes nil directly, so it
// is the rule's own fallback under test rather than DecodeBanTsCommentOptions.
//
// Each row is one of typescript-eslint's defaultOptions. The `@ts-check` row is the one a reader is
// most likely to guess backwards: `@ts-check` turns checking on, so it defaults to allowed.
func TestBanTsCommentDefaultsBindWithNoConfiguration(test *testing.T) {
	test.Parallel()

	cases := []struct {
		sourceText string
		wantIds    []string
		reason     string
	}{
		{"// @ts-ignore\n", []string{"tsIgnoreInsteadOfExpectError"}, "ts-ignore defaults to banned"},
		{"// @ts-nocheck\n", []string{"tsDirectiveComment"}, "ts-nocheck defaults to banned"},
		{"// @ts-check\n", nil, "ts-check defaults to ALLOWED"},
		{"// @ts-expect-error\n", []string{"tsDirectiveCommentRequiresDescription"},
			"ts-expect-error defaults to allow-with-description"},
		{"// @ts-expect-error ab\n", []string{"tsDirectiveCommentRequiresDescription"},
			"two characters is under the default minimum of three"},
		{"// @ts-expect-error abc\n", nil, "three characters reaches the default minimum"},
	}
	for index, testCase := range cases {
		test.Run(banTsCommentCaseName(index), func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(test, BanTsComment, banTsCommentFile, testCase.sourceText)
			// A silent row asserts clean, so the docs capture records it as clean rather than as a
			// firing case with no findings.
			if len(testCase.wantIds) == 0 {
				rule_testing.ExpectClean(test, result)
				return
			}
			rule_testing.ExpectFindings(test, result, testCase.wantIds...)
		})
	}
}

// TestDecodeBanTsCommentOptionsReadsEveryShape exercises the decoder directly.
//
// Four of the five keys accept a boolean, the string `allow-with-description`, or an object holding
// a pattern, which no struct tag can express. An absent key keeps the default; an unrecognized value
// and an unparseable pattern are refused, below. An object whose pattern is absent, null or empty
// holds no pattern, because upstream tests `option.descriptionFormat` for truth.
func TestDecodeBanTsCommentOptionsReadsEveryShape(test *testing.T) {
	test.Parallel()

	defaults := DefaultBanTsCommentOptions()

	cases := []struct {
		configuration string
		check         func(test *testing.T, options BanTsCommentOptions)
	}{
		{"{\"ts-ignore\":false}", func(test *testing.T, options BanTsCommentOptions) {
			if options.TsIgnore.Kind != BanTsCommentBoolean || options.TsIgnore.Banned {
				test.Fatalf("expected an allowing boolean, got %+v", options.TsIgnore)
			}
		}},
		{"{\"ts-check\":true}", func(test *testing.T, options BanTsCommentOptions) {
			if options.TsCheck.Kind != BanTsCommentBoolean || !options.TsCheck.Banned {
				test.Fatalf("expected a banning boolean, got %+v", options.TsCheck)
			}
		}},
		{"{\"ts-expect-error\":\"allow-with-description\"}",
			func(test *testing.T, options BanTsCommentOptions) {
				if options.TsExpectError.Kind != BanTsCommentRequireDescription {
					test.Fatalf("expected require-description, got %+v", options.TsExpectError)
				}
			}},
		{"{\"ts-nocheck\":{\"descriptionFormat\":\"^: TS\\\\d+$\"}}",
			func(test *testing.T, options BanTsCommentOptions) {
				if options.TsNoCheck.Kind != BanTsCommentDescriptionFormat {
					test.Fatalf("expected description-format, got %+v", options.TsNoCheck)
				}
				if options.TsNoCheck.DescriptionFormat == nil ||
					options.TsNoCheck.DescriptionFormat.Source() != "^: TS\\d+$" {
					test.Fatalf("pattern did not survive the decode: %+v", options.TsNoCheck)
				}
			}},
		{"{\"minimumDescriptionLength\":21}", func(test *testing.T, options BanTsCommentOptions) {
			if options.MinimumDescriptionLength != 21 {
				test.Fatalf("expected 21, got %d", options.MinimumDescriptionLength)
			}
		}},
		{"{}", func(test *testing.T, options BanTsCommentOptions) {
			if options != defaults {
				test.Fatalf("an empty object should leave every default, got %+v", options)
			}
		}},
		{"{\"ts-ignore\":{\"descriptionFormat\":\"\"}}", func(test *testing.T, options BanTsCommentOptions) {
			if options.TsIgnore.Kind != BanTsCommentDescriptionFormat || options.TsIgnore.DescriptionFormat != nil {
				test.Fatalf("an empty pattern should leave no pattern, got %+v", options.TsIgnore)
			}
		}},
		{"{\"ts-ignore\":{\"descriptionFormat\":null}}", func(test *testing.T, options BanTsCommentOptions) {
			if options.TsIgnore.Kind != BanTsCommentDescriptionFormat || options.TsIgnore.DescriptionFormat != nil {
				test.Fatalf("a null pattern should leave no pattern, got %+v", options.TsIgnore)
			}
		}},
		// A pattern only JavaScript compiles: Go's regexp has no lookahead, and ESLint accepts it.
		{"{\"ts-ignore\":{\"descriptionFormat\":\"^(?=: TS)\"}}",
			func(test *testing.T, options BanTsCommentOptions) {
				if options.TsIgnore.DescriptionFormat == nil ||
					options.TsIgnore.DescriptionFormat.Source() != "^(?=: TS)" {
					test.Fatalf("a lookahead pattern did not survive the decode: %+v", options.TsIgnore)
				}
			}},
	}
	for index, testCase := range cases {
		test.Run(banTsCommentCaseName(index), func(test *testing.T) {
			test.Parallel()
			decoded, decodeBanTsCommentOptionsError := DecodeBanTsCommentOptions([]byte(testCase.configuration))
			if decodeBanTsCommentOptionsError != nil {
				test.Fatalf("could not decode %s: %v", testCase.configuration, decodeBanTsCommentOptionsError)
			}
			testCase.check(test, decoded)
		})
	}
}

// TestDecodeBanTsCommentOptionsRefusesWhatUpstreamRefuses pins the refusals that replaced the decoder's
// fallback to the default (ruled on #pd2chkx): each value here fails typescript-eslint's schema or its
// `new RegExp`, so ESLint refuses the config, and so does cohere, naming the key.
func TestDecodeBanTsCommentOptionsRefusesWhatUpstreamRefuses(test *testing.T) {
	test.Parallel()

	cases := []struct {
		configuration string
		want          string
	}{
		{`{"ts-ignore":"nonsense"}`, `ts-ignore: expected true, false, "allow-with-description"`},
		{`{"ts-check":"Allow-with-description"}`, `ts-check: expected true, false, "allow-with-description"`},
		{`{"ts-nocheck":3}`, `ts-nocheck: expected true, false, "allow-with-description"`},
		{`{"ts-expect-error":[]}`, `ts-expect-error: expected true, false, "allow-with-description"`},
		{
			`{"ts-ignore":{"descriptionFormat":"^(unclosed"}}`,
			`ts-ignore: descriptionFormat "^(unclosed" is not a pattern JavaScript can compile`,
		},
	}
	for index, testCase := range cases {
		test.Run(banTsCommentCaseName(index), func(test *testing.T) {
			test.Parallel()
			_, decodeBanTsCommentOptionsError := DecodeBanTsCommentOptions([]byte(testCase.configuration))
			if decodeBanTsCommentOptionsError == nil ||
				!strings.Contains(decodeBanTsCommentOptionsError.Error(), testCase.want) {
				test.Fatalf(
					"decoding %s: error %v, want one containing %q",
					testCase.configuration,
					decodeBanTsCommentOptionsError,
					testCase.want,
				)
			}
		})
	}
}

// TestBanTsCommentDeclinesJavaScriptFiles pins the gate kept as a stand-in for config scope.
//
// Upstream's rule has no file-type gate; ESLint configs commonly scope it to TypeScript files. cohere's
// overrides can too, but readiness resolves cohere:adamic once for `index.ts`, so the scope cannot
// move into the set yet. The rule's doc comment has the TanStack measurement. #6aa3wrx moves the
// scope and deletes this test with the gate.
func TestBanTsCommentDeclinesJavaScriptFiles(test *testing.T) {
	test.Parallel()

	for _, fileName := range []string{
		"/repository/source/Thing.js",
		"/repository/source/Thing.jsx",
		"/repository/source/Thing.mjs",
	} {
		test.Run(fileName, func(test *testing.T) {
			test.Parallel()
			rule_testing.ExpectClean(
				test,
				rule_testing.Run(test, BanTsComment, fileName, "// @ts-ignore\n"),
			)
		})
	}
	for _, fileName := range []string{
		"/repository/source/Thing.ts",
		"/repository/source/Thing.tsx",
		"/repository/source/Thing.mts",
		"/repository/source/Thing.cts",
	} {
		test.Run(fileName, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(test, BanTsComment, fileName, "// @ts-ignore\n")
			rule_testing.ExpectFindings(test, result, "tsIgnoreInsteadOfExpectError")
		})
	}
}

// TestBanTsCommentMessageTextNamesTheDirective asserts the rendered text, exactly.
//
// Three of the four messages interpolate, and ExpectFindings sees only the id, so a format string
// that dropped the directive name or doubled a character would leave every fixture above green.
//
// Two things about how this is written. The comparison is equality on the whole description rather
// than a Contains on a fragment, because a predicate weaker than the property it guards is not a
// guard: a doubled character produces a string that still contains the needle. And every expected
// string is a literal typed here rather than a call to the rule's own message constructor, because
// comparing a finding against the constant it was reported with is equality between two values that
// move together under mutation and therefore proves nothing.
func TestBanTsCommentMessageTextNamesTheDirective(test *testing.T) {
	test.Parallel()

	cases := []struct {
		configuration string
		sourceText    string
		wantId        string
		wantText      string
	}{
		{"", "// @ts-nocheck\n", "tsDirectiveComment",
			"This file silences the TypeScript compiler with `@ts-nocheck`. A directive comment " +
				"switches off checking for a line or a whole file, so the error it hides stays in " +
				"the code and stops being reported to anyone. Fix the underlying type error " +
				"instead, or narrow the suppression to `@ts-expect-error` with a description " +
				"saying why it is necessary."},
		{"{\"ts-check\":true}", "// @ts-check\n", "tsDirectiveComment",
			"This file silences the TypeScript compiler with `@ts-check`. A directive comment " +
				"switches off checking for a line or a whole file, so the error it hides stays in " +
				"the code and stops being reported to anyone. Fix the underlying type error " +
				"instead, or narrow the suppression to `@ts-expect-error` with a description " +
				"saying why it is necessary."},
		{"", "// @ts-ignore\n", "tsIgnoreInsteadOfExpectError",
			"This suppression uses `@ts-ignore`, which keeps silencing the line even after the " +
				"underlying error is gone, so a stale suppression is invisible forever. " +
				"`@ts-expect-error` reports when the line it guards has stopped failing, which is " +
				"what makes the suppression removable."},
		{"", "// @ts-expect-error\n", "tsDirectiveCommentRequiresDescription",
			"This `@ts-expect-error` carries no explanation, so nobody reading it later can tell " +
				"whether the suppression is still needed or what it was hiding. Write at least 3 " +
				"characters after the directive saying which error it suppresses and why the " +
				"error cannot be fixed."},
		{"{\"ts-nocheck\":\"allow-with-description\",\"minimumDescriptionLength\":21}",
			"// @ts-nocheck ab\n", "tsDirectiveCommentRequiresDescription",
			"This `@ts-nocheck` carries no explanation, so nobody reading it later can tell " +
				"whether the suppression is still needed or what it was hiding. Write at least 21 " +
				"characters after the directive saying which error it suppresses and why the " +
				"error cannot be fixed."},
		{"{\"ts-ignore\":{\"descriptionFormat\":\"^: TS[0-9]+$\"}}", "// @ts-ignore: nope\n",
			"tsDirectiveCommentDescriptionNotMatchPattern",
			"The explanation after this `@ts-ignore` does not match the shape this project " +
				"requires, `^: TS[0-9]+$`. The format exists so a suppression can be traced back " +
				"to the error it hides, which a free-form note cannot be."},
	}
	for index, testCase := range cases {
		test.Run(banTsCommentCaseName(index), func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunGiven(test, BanTsComment, banTsCommentFile,
				testCase.sourceText, decodeBanTsCommentOptions(test, testCase.configuration))
			if len(result.Diagnostics) != 1 {
				test.Fatalf("expected one finding, got %d: %v", len(result.Diagnostics),
					result.MessageIds())
			}
			if result.Diagnostics[0].Message.Id != testCase.wantId {
				test.Fatalf("expected id %q, got %q", testCase.wantId,
					result.Diagnostics[0].Message.Id)
			}
			if result.Diagnostics[0].Message.Description != testCase.wantText {
				test.Fatalf("message text was\n  %q\nexpected\n  %q",
					result.Diagnostics[0].Message.Description, testCase.wantText)
			}
		})
	}
}

// TestBanTsCommentSurvivesAnUnterminatedBlockComment pins the guard in banTsCommentValue.
//
// Those guards are crash protection rather than behavioral filters, which is why the sweep reports
// them as survivors under any assertion about findings: no ExpectFindings fixture can see a panic.
// Ask what the line prevents rather than what it decides, and this is what it prevents.
//
// The reachable input is error recovery. A well-formed block comment is at least four bytes
// (`/**/`), so a naive read says the four-byte guard is dead. Probed against the parser instead: an
// unterminated `/*` at the end of a file comes back as a block comment of length two, and `/*/` and
// `/**` come back as length three. Subtracting two from each end of a two-byte comment produces a
// range whose start is past its end, and slicing the source with it panics. Measured by
// neutralizing both guards, which turns every row below into a stack trace.
//
// A file ending in an unterminated comment is not exotic. It is what a half-written file on disk
// looks like while somebody is typing, and the linter runs on those.
func TestBanTsCommentSurvivesAnUnterminatedBlockComment(test *testing.T) {
	test.Parallel()

	for _, sourceText := range []string{"/*", "/*/", "/**", "/*@ts-ignore", "//", "/*@ts-ignore\n"} {
		test.Run(sourceText, func(test *testing.T) {
			test.Parallel()
			// The assertion is that this returns at all. A finding would also be acceptable for
			// some of these; a panic is not, and a panic is what the guards prevent.
			_ = rule_testing.Run(test, BanTsComment, banTsCommentFile, sourceText)
		})
	}
}
