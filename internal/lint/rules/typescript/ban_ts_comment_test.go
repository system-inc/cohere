package typescript

import (
	"strconv"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

const banTsCommentFile = "/repository/source/Thing.ts"

// decodeBanTsCommentOptions runs a configuration through the real decoder rather than building
// the options struct directly.
//
// The decoder holds five defaults, a polymorphic value for four of the keys, and a regex
// compile, so a fixture constructing BanTsCommentOptions by hand would leave the whole config
// half of the port unexercised. An empty configuration is the bare `"error"` case, which the
// config layer turns into nil options, and it reaches the rule the same way here.
func decodeBanTsCommentOptions(t *testing.T, configuration string) any {
	t.Helper()

	if configuration == "" {
		// A rule configured as a bare `"error"` is handed nil, not an empty struct. Passing nil
		// here is what puts the rule's own fallback under test rather than the decoder's.
		return nil
	}
	options, err := DecodeBanTsCommentOptions([]byte(configuration))
	if err != nil {
		t.Fatalf("could not decode %s: %v", configuration, err)
	}
	return options
}

// TestBanTsCommentStaysSilentOnUpstreamPassCases is the imported clean corpus, verbatim.
//
// All fifty of upstream's passing inputs, extracted by the fixture tool, verified byte for byte
// against the Rust source, and written out by a script so no escape sequence passed through a
// shell or a keyboard on the way here. Every one was additionally run through the release binary,
// which reported nothing on all fifty.
func TestBanTsCommentStaysSilentOnUpstreamPassCases(t *testing.T) {
	t.Parallel()

	cases := []struct {
		configuration string
		sourceText    string
	}{
		{"", "// just a comment containing @ts-expect-error somewhere"},
		{"", "\n            /*\n            @ts-expect-error running with long description in a block\n            */\n\t\t"},
		{"", "\n            /* @ts-expect-error not on the last line\n            */\n        "},
		{"", "\n            /**\n             * @ts-expect-error not on the last line\n             */\n        "},
		{"", "\n            /* not on the last line\n            * @ts-expect-error\n            */\n        "},
		{"", "\n            /* @ts-expect-error\n            * not on the last line */\n        "},
		{"{\"ts-expect-error\": false}", "// @ts-expect-error"},
		{"{\"ts-expect-error\": \"allow-with-description\"}", "// @ts-expect-error here is why the error is expected"},
		{"{\"ts-expect-error\": \"allow-with-description\"}", "\n            /*\n            * @ts-expect-error here is why the error is expected */\n        "},
		{"{\"minimumDescriptionLength\": 21, \"ts-expect-error\": \"allow-with-description\"}", "// @ts-expect-error exactly 21 characters"},
		{"{\"minimumDescriptionLength\": 21, \"ts-expect-error\": \"allow-with-description\"}", "\n            /*\n            * @ts-expect-error exactly 21 characters*/\n        "},
		{"{\"minimumDescriptionLength\": 10, \"ts-expect-error\": {\"descriptionFormat\": \"^: TS\\\\d+ because .+$\"}}", "// @ts-expect-error: TS1234 because xyz"},
		{"{\"minimumDescriptionLength\": 10, \"ts-expect-error\": {\"descriptionFormat\": \"^: TS\\\\d+ because .+$\"}}", "\n            /*\n            * @ts-expect-error: TS1234 because xyz */\n        "},
		{"{\"ts-expect-error\": \"allow-with-description\"}", "// @ts-expect-error 👨‍👩‍👧‍👦👨‍👩‍👧‍👦👨‍👩‍👧‍👦"},
		{"", "// just a comment containing @ts-ignore somewhere"},
		{"{\"ts-ignore\": false}", "// @ts-ignore"},
		{"{\"ts-ignore\": \"allow-with-description\"}", "// @ts-ignore I think that I am exempted from any need to follow the rules!"},
		{"{\"minimumDescriptionLength\": 21, \"ts-ignore\": \"allow-with-description\"}", "\n         /*\n          @ts-ignore running with long description in a block\n         */\n\t\t"},
		{"", "\n            /*\n             @ts-ignore\n            */\n        "},
		{"", "\n            /* @ts-ignore not on the last line\n            */\n        "},
		{"", "\n            /**\n             * @ts-ignore not on the last line\n             */\n        "},
		{"", "\n            /* @ts-ignore\n            * not on the last line */\n        "},
		{"{\"minimumDescriptionLength\": 10, \"ts-ignore\": {\"descriptionFormat\": \"^: TS\\\\d+ because .+$\"}}", "// @ts-ignore: TS1234 because xyz"},
		{"{\"ts-ignore\": \"allow-with-description\"}", "// @ts-ignore 👨‍👩‍👧‍👦👨‍👩‍👧‍👦👨‍👩‍👧‍👦"},
		{"{\"ts-ignore\": \"allow-with-description\"}", "\n            /*\n            * @ts-ignore here is why the error is expected */\n        "},
		{"{\"minimumDescriptionLength\": 21, \"ts-ignore\": \"allow-with-description\"}", "// @ts-ignore exactly 21 characters"},
		{"{\"minimumDescriptionLength\": 21, \"ts-ignore\": \"allow-with-description\"}", "\n            /*\n            * @ts-ignore exactly 21 characters*/\n        "},
		{"{\"minimumDescriptionLength\": 10, \"ts-ignore\": {\"descriptionFormat\": \"^: TS\\\\d+ because .+$\"}}", "\n            /*\n            * @ts-ignore: TS1234 because xyz */\n        "},
		{"", "// just a comment containing @ts-nocheck somewhere"},
		{"{\"ts-nocheck\": false}", "// @ts-nocheck"},
		{"{\"ts-nocheck\": \"allow-with-description\"}", "// @ts-nocheck no doubt, people will put nonsense here from time to time just to get the rule to stop reporting, perhaps even long messages with other nonsense in them like other // @ts-nocheck or // @ts-ignore things"},
		{"{\"minimumDescriptionLength\": 21, \"ts-nocheck\": \"allow-with-description\"}", "\n        /*\n            @ts-nocheck running with long description in a block\n        */"},
		{"{\"minimumDescriptionLength\": 10, \"ts-nocheck\": {\"descriptionFormat\": \"^: TS\\\\d+ because .+$\"}}", "// @ts-nocheck: TS1234 because xyz"},
		{"{\"ts-nocheck\": \"allow-with-description\"}", "// @ts-nocheck 👨‍👩‍👧‍👦👨‍👩‍👧‍👦👨‍👩‍👧‍👦"},
		{"", "//// @ts-nocheck - pragma comments may contain 2 or 3 leading slashes"},
		{"", "\n            /**\n             @ts-nocheck\n            */\n        "},
		{"", "\n            /*\n             @ts-nocheck\n            */\n        "},
		{"", "/** @ts-nocheck */"},
		{"", "/* @ts-nocheck */"},
		{"", "// just a comment containing @ts-check somewhere"},
		{"", "\n        /*\n            @ts-check running with long description in a block\n        */\n        "},
		{"{\"ts-check\": false}", "// @ts-check"},
		{"{\"minimumDescriptionLength\": 3, \"ts-check\": \"allow-with-description\"}", "// @ts-check with a description and also with a no-op // @ts-ignore"},
		{"{\"minimumDescriptionLength\": 10, \"ts-check\": {\"descriptionFormat\": \"^: TS\\\\d+ because .+$\"}}", "// @ts-check: TS1234 because xyz"},
		{"{\"ts-check\": \"allow-with-description\"}", "// @ts-check 👨‍👩‍👧‍👦👨‍👩‍👧‍👦👨‍👩‍👧‍👦"},
		{"{\"ts-check\": true}", "//// @ts-check - pragma comments may contain 2 or 3 leading slashes"},
		{"{\"ts-check\": true}", "\n            /**\n             @ts-check\n            */\n        "},
		{"{\"ts-check\": true}", "\n            /*\n             @ts-check\n            */\n        "},
		{"{\"ts-check\": true}", "/** @ts-check */"},
		{"{\"ts-check\": true}", "/* @ts-check */"},
	}
	for index, testCase := range cases {
		t.Run(banTsCommentCaseName(index), func(t *testing.T) {
			t.Parallel()
			rule_testing.ExpectClean(t, rule_testing.RunWithOptions(t, BanTsComment,
				banTsCommentFile, testCase.sourceText,
				decodeBanTsCommentOptions(t, testCase.configuration)))
		})
	}
}

// TestBanTsCommentFiresOnUpstreamFailCases is the imported reporting corpus, verbatim.
//
// The snapshot records fifty-two diagnostics against these fifty-two inputs, so the extractor
// reported no discrepancy. The message id on each row is not derived from reading the Rust: every
// case was run through the release binary and the arm it took was read off the output, which is
// what separates the four arms this rule can reach.
func TestBanTsCommentFiresOnUpstreamFailCases(t *testing.T) {
	t.Parallel()

	cases := []struct {
		configuration string
		sourceText    string
		wantIds       []string
	}{
		{"{\"ts-expect-error\": true}", "// @ts-expect-error", []string{"banTsComment"}},
		{"{\"ts-expect-error\": true}", "/* @ts-expect-error */", []string{"banTsComment"}},
		{"{\"ts-expect-error\": true}", "\n/*\n @ts-expect-error */\n        ", []string{"banTsComment"}},
		{"{\"ts-expect-error\": true}", "\n/** on the last line\n @ts-expect-error */\n        ", []string{"banTsComment"}},
		{"{\"ts-expect-error\": true}", "\n/** on the last line\n * @ts-expect-error */\n        ", []string{"banTsComment"}},
		{"{\"minimumDescriptionLength\": 10, \"ts-expect-error\": \"allow-with-description\"}", "\n/**\n * @ts-expect-error: TODO */\n        ", []string{"banTsCommentRequiresDescription"}},
		{"{\"minimumDescriptionLength\": 25, \"ts-expect-error\": {\"descriptionFormat\": \"^: TS\\\\d+ because .+$\"}}", "\n/**\n * @ts-expect-error: TS1234 because xyz */\n        ", []string{"banTsCommentRequiresDescription"}},
		{"{\"ts-expect-error\": {\"descriptionFormat\": \"^: TS\\\\d+ because .+$\"}}", "\n/**\n * @ts-expect-error: TS1234 */\n        ", []string{"banTsCommentDescriptionFormat"}},
		{"{\"ts-expect-error\": {\"descriptionFormat\": \"^: TS\\\\d+ because .+$\"}}", "\n/**\n * @ts-expect-error    : TS1234 */\n        ", []string{"banTsCommentDescriptionFormat"}},
		{"{\"ts-expect-error\": true}", "/** @ts-expect-error */", []string{"banTsComment"}},
		{"{\"ts-expect-error\": true}", "// @ts-expect-error: Suppress next line", []string{"banTsComment"}},
		{"{\"ts-expect-error\": true}", "/////@ts-expect-error: Suppress next line", []string{"banTsComment"}},
		{"{\"ts-expect-error\": true}", "\nif (false) {\n    // @ts-expect-error: Unreachable code error\n    console.log('hello');\n}\n          ", []string{"banTsComment"}},
		{"{\"ts-expect-error\": \"allow-with-description\"}", "// @ts-expect-error", []string{"banTsCommentRequiresDescription"}},
		{"{\"minimumDescriptionLength\": 10, \"ts-expect-error\": \"allow-with-description\"}", "// @ts-expect-error: TODO", []string{"banTsCommentRequiresDescription"}},
		{"{\"minimumDescriptionLength\": 25, \"ts-expect-error\": {\"descriptionFormat\": \"^: TS\\\\d+ because .+$\"}}", "// @ts-expect-error: TS1234 because xyz", []string{"banTsCommentRequiresDescription"}},
		{"{\"ts-expect-error\": {\"descriptionFormat\": \"^: TS\\\\d+ because .+$\"}}", "// @ts-expect-error: TS1234", []string{"banTsCommentDescriptionFormat"}},
		{"{\"ts-expect-error\": {\"descriptionFormat\": \"^: TS\\\\d+ because .+$\"}}", "// @ts-expect-error    : TS1234 because xyz", []string{"banTsCommentDescriptionFormat"}},
		{"{\"ts-expect-error\": true, \"ts-ignore\": true}", "// @ts-ignore", []string{"banTsCommentPreferExpectError"}},
		{"{\"ts-expect-error\": \"allow-with-description\", \"ts-ignore\": true}", "// @ts-ignore", []string{"banTsCommentPreferExpectError"}},
		{"", "// @ts-ignore", []string{"banTsCommentPreferExpectError"}},
		{"{\"ts-ignore\": true}", "/* @ts-ignore */", []string{"banTsCommentPreferExpectError"}},
		{"{\"ts-ignore\": true}", "\n/*\n @ts-ignore */\n            ", []string{"banTsCommentPreferExpectError"}},
		{"{\"ts-ignore\": true}", "\n/** on the last line\n @ts-ignore */\n            ", []string{"banTsCommentPreferExpectError"}},
		{"{\"ts-ignore\": true}", "\n/** on the last line\n * @ts-ignore */\n            ", []string{"banTsCommentPreferExpectError"}},
		{"{\"ts-expect-error\": false, \"ts-ignore\": true}", "/** @ts-ignore */", []string{"banTsCommentPreferExpectError"}},
		{"{\"minimumDescriptionLength\": 10, \"ts-expect-error\": \"allow-with-description\"}", "\n/**\n * @ts-ignore: TODO */\n            ", []string{"banTsCommentPreferExpectError"}},
		{"{\"minimumDescriptionLength\": 25, \"ts-expect-error\": {\"descriptionFormat\": \"^: TS\\\\d+ because .+$\"}}", "\n/**\n * @ts-ignore: TS1234 because xyz */\n            ", []string{"banTsCommentPreferExpectError"}},
		{"", "// @ts-ignore: Suppress next line", []string{"banTsCommentPreferExpectError"}},
		{"", "/////@ts-ignore: Suppress next line", []string{"banTsCommentPreferExpectError"}},
		{"", "\nif (false) {\n    // @ts-ignore: Unreachable code error\n    console.log('hello');\n}\n            ", []string{"banTsCommentPreferExpectError"}},
		{"{\"ts-ignore\": \"allow-with-description\"}", "// @ts-ignore", []string{"banTsCommentRequiresDescription"}},
		{"{\"ts-ignore\": \"allow-with-description\"}", "// @ts-ignore         ", []string{"banTsCommentRequiresDescription"}},
		{"{\"ts-ignore\": \"allow-with-description\"}", "// @ts-ignore    .", []string{"banTsCommentRequiresDescription"}},
		{"{\"minimumDescriptionLength\": 25, \"ts-ignore\": {\"descriptionFormat\": \"^: TS\\\\d+ because .+$\"}}", "// @ts-ignore: TS1234 because xyz", []string{"banTsCommentRequiresDescription"}},
		{"{\"ts-ignore\": {\"descriptionFormat\": \"^: TS\\\\d+ because .+$\"}}", "// @ts-ignore: TS1234", []string{"banTsCommentDescriptionFormat"}},
		{"{\"ts-ignore\": {\"descriptionFormat\": \"^: TS\\\\d+ because .+$\"}}", "// @ts-ignore    : TS1234 because xyz", []string{"banTsCommentDescriptionFormat"}},
		{"{\"ts-nocheck\": true}", "// @ts-nocheck", []string{"banTsComment"}},
		{"", "// @ts-nocheck", []string{"banTsComment"}},
		{"", "// @ts-nocheck: Suppress next line", []string{"banTsComment"}},
		{"", "\nif (false) {\n    // @ts-nocheck: Unreachable code error\n    console.log('hello');\n}\n            ", []string{"banTsComment"}},
		{"{\"ts-nocheck\": \"allow-with-description\"}", "// @ts-nocheck", []string{"banTsCommentRequiresDescription"}},
		{"{\"minimumDescriptionLength\": 25, \"ts-nocheck\": {\"descriptionFormat\": \"^: TS\\\\d+ because .+$\"}}", "// @ts-nocheck: TS1234 because xyz", []string{"banTsCommentRequiresDescription"}},
		{"{\"ts-nocheck\": {\"descriptionFormat\": \"^: TS\\\\d+ because .+$\"}}", "// @ts-nocheck: TS1234", []string{"banTsCommentDescriptionFormat"}},
		{"{\"ts-nocheck\": {\"descriptionFormat\": \"^: TS\\\\d+ because .+$\"}}", "// @ts-nocheck    : TS1234 because xyz", []string{"banTsCommentDescriptionFormat"}},
		{"{\"ts-check\": true}", "// @ts-check", []string{"banTsComment"}},
		{"{\"ts-check\": true}", "// @ts-check: Suppress next line", []string{"banTsComment"}},
		{"{\"ts-check\": true}", "\nif (false) {\n    // @ts-check: Unreachable code error\n    console.log('hello');\n}\n            ", []string{"banTsComment"}},
		{"{\"ts-check\": \"allow-with-description\"}", "// @ts-check", []string{"banTsCommentRequiresDescription"}},
		{"{\"minimumDescriptionLength\": 25, \"ts-check\": {\"descriptionFormat\": \"^: TS\\\\d+ because .+$\"}}", "// @ts-check: TS1234 because xyz", []string{"banTsCommentRequiresDescription"}},
		{"{\"ts-check\": {\"descriptionFormat\": \"^: TS\\\\d+ because .+$\"}}", "// @ts-check: TS1234", []string{"banTsCommentDescriptionFormat"}},
		{"{\"ts-check\": {\"descriptionFormat\": \"^: TS\\\\d+ because .+$\"}}", "// @ts-check    : TS1234 because xyz", []string{"banTsCommentDescriptionFormat"}},
	}
	for index, testCase := range cases {
		t.Run(banTsCommentCaseName(index), func(t *testing.T) {
			t.Parallel()
			result := rule_testing.RunWithOptions(t, BanTsComment, banTsCommentFile,
				testCase.sourceText, decodeBanTsCommentOptions(t, testCase.configuration))
			rule_testing.ExpectFindings(t, result, testCase.wantIds...)
		})
	}
}

// TestBanTsCommentPointsAtTheCommentInterior asserts the span of every imported reporting case.
//
// ExpectFindings sees the message id and the count and nothing else, so a rule pointing at the
// whole comment rather than its interior passes the fixture above while being wrong everywhere.
// Each expected span below is the text the RELEASE BINARY's own label covers, read out of its
// JSON reporter, so this pins the port against upstream rather than against itself.
func TestBanTsCommentPointsAtTheCommentInterior(t *testing.T) {
	t.Parallel()

	cases := []struct {
		configuration string
		sourceText    string
		wantReported  []string
	}{
		{"{\"ts-expect-error\": true}", "// @ts-expect-error", []string{" @ts-expect-error"}},
		{"{\"ts-expect-error\": true}", "/* @ts-expect-error */", []string{" @ts-expect-error "}},
		{"{\"ts-expect-error\": true}", "\n/*\n @ts-expect-error */\n        ", []string{"\n @ts-expect-error "}},
		{"{\"ts-expect-error\": true}", "\n/** on the last line\n @ts-expect-error */\n        ", []string{"* on the last line\n @ts-expect-error "}},
		{"{\"ts-expect-error\": true}", "\n/** on the last line\n * @ts-expect-error */\n        ", []string{"* on the last line\n * @ts-expect-error "}},
		{"{\"minimumDescriptionLength\": 10, \"ts-expect-error\": \"allow-with-description\"}", "\n/**\n * @ts-expect-error: TODO */\n        ", []string{"*\n * @ts-expect-error: TODO "}},
		{"{\"minimumDescriptionLength\": 25, \"ts-expect-error\": {\"descriptionFormat\": \"^: TS\\\\d+ because .+$\"}}", "\n/**\n * @ts-expect-error: TS1234 because xyz */\n        ", []string{"*\n * @ts-expect-error: TS1234 because xyz "}},
		{"{\"ts-expect-error\": {\"descriptionFormat\": \"^: TS\\\\d+ because .+$\"}}", "\n/**\n * @ts-expect-error: TS1234 */\n        ", []string{"*\n * @ts-expect-error: TS1234 "}},
		{"{\"ts-expect-error\": {\"descriptionFormat\": \"^: TS\\\\d+ because .+$\"}}", "\n/**\n * @ts-expect-error    : TS1234 */\n        ", []string{"*\n * @ts-expect-error    : TS1234 "}},
		{"{\"ts-expect-error\": true}", "/** @ts-expect-error */", []string{"* @ts-expect-error "}},
		{"{\"ts-expect-error\": true}", "// @ts-expect-error: Suppress next line", []string{" @ts-expect-error: Suppress next line"}},
		{"{\"ts-expect-error\": true}", "/////@ts-expect-error: Suppress next line", []string{"///@ts-expect-error: Suppress next line"}},
		{"{\"ts-expect-error\": true}", "\nif (false) {\n    // @ts-expect-error: Unreachable code error\n    console.log('hello');\n}\n          ", []string{" @ts-expect-error: Unreachable code error"}},
		{"{\"ts-expect-error\": \"allow-with-description\"}", "// @ts-expect-error", []string{" @ts-expect-error"}},
		{"{\"minimumDescriptionLength\": 10, \"ts-expect-error\": \"allow-with-description\"}", "// @ts-expect-error: TODO", []string{" @ts-expect-error: TODO"}},
		{"{\"minimumDescriptionLength\": 25, \"ts-expect-error\": {\"descriptionFormat\": \"^: TS\\\\d+ because .+$\"}}", "// @ts-expect-error: TS1234 because xyz", []string{" @ts-expect-error: TS1234 because xyz"}},
		{"{\"ts-expect-error\": {\"descriptionFormat\": \"^: TS\\\\d+ because .+$\"}}", "// @ts-expect-error: TS1234", []string{" @ts-expect-error: TS1234"}},
		{"{\"ts-expect-error\": {\"descriptionFormat\": \"^: TS\\\\d+ because .+$\"}}", "// @ts-expect-error    : TS1234 because xyz", []string{" @ts-expect-error    : TS1234 because xyz"}},
		{"{\"ts-expect-error\": true, \"ts-ignore\": true}", "// @ts-ignore", []string{" @ts-ignore"}},
		{"{\"ts-expect-error\": \"allow-with-description\", \"ts-ignore\": true}", "// @ts-ignore", []string{" @ts-ignore"}},
		{"", "// @ts-ignore", []string{" @ts-ignore"}},
		{"{\"ts-ignore\": true}", "/* @ts-ignore */", []string{" @ts-ignore "}},
		{"{\"ts-ignore\": true}", "\n/*\n @ts-ignore */\n            ", []string{"\n @ts-ignore "}},
		{"{\"ts-ignore\": true}", "\n/** on the last line\n @ts-ignore */\n            ", []string{"* on the last line\n @ts-ignore "}},
		{"{\"ts-ignore\": true}", "\n/** on the last line\n * @ts-ignore */\n            ", []string{"* on the last line\n * @ts-ignore "}},
		{"{\"ts-expect-error\": false, \"ts-ignore\": true}", "/** @ts-ignore */", []string{"* @ts-ignore "}},
		{"{\"minimumDescriptionLength\": 10, \"ts-expect-error\": \"allow-with-description\"}", "\n/**\n * @ts-ignore: TODO */\n            ", []string{"*\n * @ts-ignore: TODO "}},
		{"{\"minimumDescriptionLength\": 25, \"ts-expect-error\": {\"descriptionFormat\": \"^: TS\\\\d+ because .+$\"}}", "\n/**\n * @ts-ignore: TS1234 because xyz */\n            ", []string{"*\n * @ts-ignore: TS1234 because xyz "}},
		{"", "// @ts-ignore: Suppress next line", []string{" @ts-ignore: Suppress next line"}},
		{"", "/////@ts-ignore: Suppress next line", []string{"///@ts-ignore: Suppress next line"}},
		{"", "\nif (false) {\n    // @ts-ignore: Unreachable code error\n    console.log('hello');\n}\n            ", []string{" @ts-ignore: Unreachable code error"}},
		{"{\"ts-ignore\": \"allow-with-description\"}", "// @ts-ignore", []string{" @ts-ignore"}},
		{"{\"ts-ignore\": \"allow-with-description\"}", "// @ts-ignore         ", []string{" @ts-ignore         "}},
		{"{\"ts-ignore\": \"allow-with-description\"}", "// @ts-ignore    .", []string{" @ts-ignore    ."}},
		{"{\"minimumDescriptionLength\": 25, \"ts-ignore\": {\"descriptionFormat\": \"^: TS\\\\d+ because .+$\"}}", "// @ts-ignore: TS1234 because xyz", []string{" @ts-ignore: TS1234 because xyz"}},
		{"{\"ts-ignore\": {\"descriptionFormat\": \"^: TS\\\\d+ because .+$\"}}", "// @ts-ignore: TS1234", []string{" @ts-ignore: TS1234"}},
		{"{\"ts-ignore\": {\"descriptionFormat\": \"^: TS\\\\d+ because .+$\"}}", "// @ts-ignore    : TS1234 because xyz", []string{" @ts-ignore    : TS1234 because xyz"}},
		{"{\"ts-nocheck\": true}", "// @ts-nocheck", []string{" @ts-nocheck"}},
		{"", "// @ts-nocheck", []string{" @ts-nocheck"}},
		{"", "// @ts-nocheck: Suppress next line", []string{" @ts-nocheck: Suppress next line"}},
		{"", "\nif (false) {\n    // @ts-nocheck: Unreachable code error\n    console.log('hello');\n}\n            ", []string{" @ts-nocheck: Unreachable code error"}},
		{"{\"ts-nocheck\": \"allow-with-description\"}", "// @ts-nocheck", []string{" @ts-nocheck"}},
		{"{\"minimumDescriptionLength\": 25, \"ts-nocheck\": {\"descriptionFormat\": \"^: TS\\\\d+ because .+$\"}}", "// @ts-nocheck: TS1234 because xyz", []string{" @ts-nocheck: TS1234 because xyz"}},
		{"{\"ts-nocheck\": {\"descriptionFormat\": \"^: TS\\\\d+ because .+$\"}}", "// @ts-nocheck: TS1234", []string{" @ts-nocheck: TS1234"}},
		{"{\"ts-nocheck\": {\"descriptionFormat\": \"^: TS\\\\d+ because .+$\"}}", "// @ts-nocheck    : TS1234 because xyz", []string{" @ts-nocheck    : TS1234 because xyz"}},
		{"{\"ts-check\": true}", "// @ts-check", []string{" @ts-check"}},
		{"{\"ts-check\": true}", "// @ts-check: Suppress next line", []string{" @ts-check: Suppress next line"}},
		{"{\"ts-check\": true}", "\nif (false) {\n    // @ts-check: Unreachable code error\n    console.log('hello');\n}\n            ", []string{" @ts-check: Unreachable code error"}},
		{"{\"ts-check\": \"allow-with-description\"}", "// @ts-check", []string{" @ts-check"}},
		{"{\"minimumDescriptionLength\": 25, \"ts-check\": {\"descriptionFormat\": \"^: TS\\\\d+ because .+$\"}}", "// @ts-check: TS1234 because xyz", []string{" @ts-check: TS1234 because xyz"}},
		{"{\"ts-check\": {\"descriptionFormat\": \"^: TS\\\\d+ because .+$\"}}", "// @ts-check: TS1234", []string{" @ts-check: TS1234"}},
		{"{\"ts-check\": {\"descriptionFormat\": \"^: TS\\\\d+ because .+$\"}}", "// @ts-check    : TS1234 because xyz", []string{" @ts-check    : TS1234 because xyz"}},
	}
	for index, testCase := range cases {
		t.Run(banTsCommentCaseName(index), func(t *testing.T) {
			t.Parallel()
			result := rule_testing.RunWithOptions(t, BanTsComment, banTsCommentFile,
				testCase.sourceText, decodeBanTsCommentOptions(t, testCase.configuration))
			if len(result.Diagnostics) != len(testCase.wantReported) {
				t.Fatalf("expected %d findings, got %d", len(testCase.wantReported), len(result.Diagnostics))
			}
			for position, want := range testCase.wantReported {
				found := result.Diagnostics[position].Range
				got := testCase.sourceText[found.Pos():found.End()]
				if got != want {
					t.Fatalf("finding %d pointed at %q, expected %q", position, got, want)
				}
			}
		})
	}
}

// banTsCommentCaseName names a subtest by its index in the corpus.
//
// By index rather than by source text, deliberately. Several cases differ only in whitespace or
// in case, and a subtest name built from the source would collide, which reads as a smaller
// corpus rather than as an error.
func banTsCommentCaseName(index int) string {
	return "case" + strconv.Itoa(index)
}

// TestBanTsCommentRewritesEveryIgnoreInTheComment asserts the repair upstream ships.
//
// The first five rows are upstream's own `fix` vector, which the extractor counted and which no
// message-id fixture can see: a finding carrying a rewrite that deletes the wrong range satisfies
// ExpectFindings exactly as well as a correct one. The sixth row is not upstream's, and it is the
// reason the rule spells its replacement as a replace-all: the fixer is `cow_replace`, so a comment
// naming the directive twice has BOTH rewritten. Measured on the release binary with `--fix`,
// which produced `// @ts-expect-error see other @ts-expect-error` rather than rewriting only the
// directive at the front.
func TestBanTsCommentRewritesEveryIgnoreInTheComment(t *testing.T) {
	t.Parallel()

	cases := []struct {
		sourceText string
		wantSource string
	}{
		{"// @ts-ignore", "// @ts-expect-error"},
		{"/* @ts-ignore */", "/* @ts-expect-error */"},
		{"// @ts-ignore: TS1234 because xyz", "// @ts-expect-error: TS1234 because xyz"},
		{"// @ts-ignore: TS1234", "// @ts-expect-error: TS1234"},
		{"// @ts-ignore    : TS1234 because xyz", "// @ts-expect-error    : TS1234 because xyz"},
		{"// @ts-ignore see other @ts-ignore", "// @ts-expect-error see other @ts-expect-error"},
	}
	for index, testCase := range cases {
		t.Run(banTsCommentCaseName(index), func(t *testing.T) {
			t.Parallel()
			result := rule_testing.RunWithOptions(t, BanTsComment, banTsCommentFile,
				testCase.sourceText, nil)
			rule_testing.ExpectFixedSource(t, result, testCase.wantSource)
		})
	}
}

// TestBanTsCommentOnlyIgnoreCarriesAFix pins which arm proposes a repair.
//
// Three of the four message arms report with no fix, and only `@ts-ignore` under a boolean ban
// offers one, because it is the only case with a meaning-preserving answer. A rule proposing a
// rewrite on the other arms would delete a directive the author still needs, and the fix engine
// applies unattended, so this is asserted rather than left to the reader.
func TestBanTsCommentOnlyIgnoreCarriesAFix(t *testing.T) {
	t.Parallel()

	cases := []struct {
		configuration string
		sourceText    string
		wantFixes     int
	}{
		{"", "// @ts-ignore", 1},
		{"", "// @ts-nocheck", 0},
		{"", "// @ts-expect-error", 0},
		{"{\"ts-check\":true}", "// @ts-check", 0},
		{"{\"ts-ignore\":\"allow-with-description\"}", "// @ts-ignore", 0},
	}
	for index, testCase := range cases {
		t.Run(banTsCommentCaseName(index), func(t *testing.T) {
			t.Parallel()
			result := rule_testing.RunWithOptions(t, BanTsComment, banTsCommentFile,
				testCase.sourceText, decodeBanTsCommentOptions(t, testCase.configuration))
			if len(result.Diagnostics) != 1 {
				t.Fatalf("expected one finding, got %d", len(result.Diagnostics))
			}
			if len(result.Diagnostics[0].Fixes) != testCase.wantFixes {
				t.Fatalf("expected %d fixes, got %d", testCase.wantFixes,
					len(result.Diagnostics[0].Fixes))
			}
		})
	}
}

// TestBanTsCommentReadsTheDirectivePrefixAlphabet covers the guard on what may precede `@ts-`.
//
// Not one of these inputs is in the imported corpus, and every verdict below was read off the
// release binary rather than off the Rust. The guard is the whole reason upstream's first passing
// case is clean, and reading the code leaves it ambiguous whether the test is on position or on the
// surrounding text: it is on the text, and the alphabet differs between a line comment and a block
// one, which is a distinction no imported fixture exercises.
func TestBanTsCommentReadsTheDirectivePrefixAlphabet(t *testing.T) {
	t.Parallel()

	cases := []struct {
		sourceText string
		wantIds    []string
		reason     string
	}{
		{"//@ts-ignore\n", []string{"banTsCommentPreferExpectError"},
			"no space is required after the slashes"},
		{"// * @ts-ignore\n", nil,
			"a star may not precede the directive in a LINE comment"},
		{"/* * @ts-ignore */\n", []string{"banTsCommentPreferExpectError"},
			"a star may precede it in a BLOCK comment, which is the asymmetry"},
		{"/* / @ts-ignore */\n", []string{"banTsCommentPreferExpectError"},
			"so may a slash, in a block comment"},
		{"// hello @ts-ignore\n", nil,
			"a word before the directive declines the whole comment"},
		{"/* @ts-ignore\n * not the last line */\n", nil,
			"only the final line of a block comment is scanned"},
		{"if (false) {\n    // @ts-ignore: x\n    console.log('hello');\n}\n",
			[]string{"banTsCommentPreferExpectError"},
			"a directive nested inside a block is reachable through the comment scan"},
	}
	for index, testCase := range cases {
		t.Run(banTsCommentCaseName(index), func(t *testing.T) {
			t.Parallel()
			result := rule_testing.RunWithOptions(t, BanTsComment, banTsCommentFile,
				testCase.sourceText, nil)
			rule_testing.ExpectFindings(t, result, testCase.wantIds...)
		})
	}
}

// TestBanTsCommentExemptsPragmasForCheckAndNoCheckOnly covers the three-slash guard.
//
// The asymmetry is the surprising half and it is measured, not reasoned: a leading slash in the
// comment's content exempts `check` and `nocheck` while leaving `ignore` and `expect-error`
// reporting. Upstream's corpus writes only the two exempt spellings, so the two reporting rows here
// are the ones that pin the guard to the directives it actually names.
func TestBanTsCommentExemptsPragmasForCheckAndNoCheckOnly(t *testing.T) {
	t.Parallel()

	cases := []struct {
		configuration string
		sourceText    string
		wantIds       []string
	}{
		{"", "/// @ts-nocheck\n", nil},
		{"{\"ts-check\":true}", "/// @ts-check\n", nil},
		{"", "/// @ts-ignore\n", []string{"banTsCommentPreferExpectError"}},
		{"", "/// @ts-expect-error\n", []string{"banTsCommentRequiresDescription"}},
		{"", "/* @ts-nocheck */\n", nil},
		{"", "// @ts-nocheck\n", []string{"banTsComment"}},
	}
	for index, testCase := range cases {
		t.Run(banTsCommentCaseName(index), func(t *testing.T) {
			t.Parallel()
			result := rule_testing.RunWithOptions(t, BanTsComment, banTsCommentFile,
				testCase.sourceText, decodeBanTsCommentOptions(t, testCase.configuration))
			rule_testing.ExpectFindings(t, result, testCase.wantIds...)
		})
	}
}

// TestBanTsCommentMeasuresTheDescriptionInBytes pins the length unit.
//
// Rust's `str::len` is a byte count, so a two-rune four-byte description satisfies a minimum of
// three while a one-rune two-byte one does not. A port counting runes would report the first, and
// no imported fixture could see the difference: upstream's three emoji cases are all far past the
// minimum either way. Both rows measured on the release binary.
func TestBanTsCommentMeasuresTheDescriptionInBytes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		sourceText string
		wantIds    []string
	}{
		{"// @ts-expect-error éé\n", nil},
		{"// @ts-expect-error é\n", []string{"banTsCommentRequiresDescription"}},
	}
	for index, testCase := range cases {
		t.Run(banTsCommentCaseName(index), func(t *testing.T) {
			t.Parallel()
			result := rule_testing.RunWithOptions(t, BanTsComment, banTsCommentFile,
				testCase.sourceText, nil)
			rule_testing.ExpectFindings(t, result, testCase.wantIds...)
		})
	}
}

// TestBanTsCommentReportsLengthAndFormatIndependently pins that one comment can report twice.
//
// The two checks run in sequence with no early exit, so a description that is both too short and
// wrongly shaped produces two findings against one span. No corpus case reaches this, which is the
// only reason the extractor's fifty-two diagnostics equal its fifty-two inputs, so this fixture is
// written here rather than imported. Measured on the release binary, which emitted both.
func TestBanTsCommentReportsLengthAndFormatIndependently(t *testing.T) {
	t.Parallel()

	const configuration = "{\"ts-ignore\":{\"descriptionFormat\":\"^: TS\\\\d+ because .+$\"}," +
		"\"minimumDescriptionLength\":25}"

	result := rule_testing.RunWithOptions(t, BanTsComment, banTsCommentFile,
		"// @ts-ignore: TS1\n", decodeBanTsCommentOptions(t, configuration))
	rule_testing.ExpectFindings(t, result,
		"banTsCommentRequiresDescription", "banTsCommentDescriptionFormat")
}

// TestBanTsCommentMatchesTheFormatAgainstTheUntrimmedDescription pins the other half of that pair.
//
// The length check trims and the pattern match does not, so a description padded with spaces
// satisfies the first and fails the second. That asymmetry is what makes upstream's four
// `@ts-ignore    : TS1234` shaped cases report, and a port trimming for both would silence all
// four while passing every clean case.
func TestBanTsCommentMatchesTheFormatAgainstTheUntrimmedDescription(t *testing.T) {
	t.Parallel()

	const configuration = "{\"ts-ignore\":{\"descriptionFormat\":\"^: TS\\\\d+ because .+$\"}}"

	padded := rule_testing.RunWithOptions(t, BanTsComment, banTsCommentFile,
		"// @ts-ignore    : TS1234 because xyz\n", decodeBanTsCommentOptions(t, configuration))
	rule_testing.ExpectFindings(t, padded, "banTsCommentDescriptionFormat")

	unpadded := rule_testing.RunWithOptions(t, BanTsComment, banTsCommentFile,
		"// @ts-ignore: TS1234 because xyz\n", decodeBanTsCommentOptions(t, configuration))
	rule_testing.ExpectClean(t, unpadded)
}

// TestBanTsCommentDefaultsBindWithNoConfiguration is the fixture that bypasses the decoder.
//
// A rule configured as a bare `"error"` is handed nil options, and a bare type assertion on nil
// yields four zero-valued settings that match no arm, which registers the rule on every file and
// reports nothing while every decoder-routed fixture stays green. This passes nil directly, so it
// is the rule's own fallback under test rather than DecodeBanTsCommentOptions.
//
// Each row is a default pinned against the release binary rather than read off the inventory, which
// records `"options": "no"` for this rule and is wrong. The `@ts-check` row is the one a reader is
// most likely to guess backwards: `@ts-check` turns checking ON, so it defaults to allowed.
func TestBanTsCommentDefaultsBindWithNoConfiguration(t *testing.T) {
	t.Parallel()

	cases := []struct {
		sourceText string
		wantIds    []string
		reason     string
	}{
		{"// @ts-ignore\n", []string{"banTsCommentPreferExpectError"}, "ts-ignore defaults to banned"},
		{"// @ts-nocheck\n", []string{"banTsComment"}, "ts-nocheck defaults to banned"},
		{"// @ts-check\n", nil, "ts-check defaults to ALLOWED"},
		{"// @ts-expect-error\n", []string{"banTsCommentRequiresDescription"},
			"ts-expect-error defaults to allow-with-description"},
		{"// @ts-expect-error ab\n", []string{"banTsCommentRequiresDescription"},
			"two bytes is under the default minimum of three"},
		{"// @ts-expect-error abc\n", nil, "three bytes reaches the default minimum"},
	}
	for index, testCase := range cases {
		t.Run(banTsCommentCaseName(index), func(t *testing.T) {
			t.Parallel()
			result := rule_testing.RunWithOptions(t, BanTsComment, banTsCommentFile,
				testCase.sourceText, nil)
			rule_testing.ExpectFindings(t, result, testCase.wantIds...)
		})
	}
}

// TestDecodeBanTsCommentOptionsReadsEveryShape exercises the decoder directly.
//
// Four of the five keys accept a boolean, the string `allow-with-description`, or an object holding
// a pattern, which is upstream's hand-written Deserialize and which no struct tag can express. An
// absent key keeps the default; an unrecognized value and an unparseable pattern are refused, below.
func TestDecodeBanTsCommentOptionsReadsEveryShape(t *testing.T) {
	t.Parallel()

	defaults := DefaultBanTsCommentOptions()

	cases := []struct {
		configuration string
		check         func(t *testing.T, options BanTsCommentOptions)
	}{
		{"{\"ts-ignore\":false}", func(t *testing.T, options BanTsCommentOptions) {
			if options.TsIgnore.Kind != BanTsCommentBoolean || options.TsIgnore.Banned {
				t.Fatalf("expected an allowing boolean, got %+v", options.TsIgnore)
			}
		}},
		{"{\"ts-check\":true}", func(t *testing.T, options BanTsCommentOptions) {
			if options.TsCheck.Kind != BanTsCommentBoolean || !options.TsCheck.Banned {
				t.Fatalf("expected a banning boolean, got %+v", options.TsCheck)
			}
		}},
		{"{\"ts-expect-error\":\"allow-with-description\"}",
			func(t *testing.T, options BanTsCommentOptions) {
				if options.TsExpectError.Kind != BanTsCommentRequireDescription {
					t.Fatalf("expected require-description, got %+v", options.TsExpectError)
				}
			}},
		{"{\"ts-nocheck\":{\"descriptionFormat\":\"^: TS\\\\d+$\"}}",
			func(t *testing.T, options BanTsCommentOptions) {
				if options.TsNoCheck.Kind != BanTsCommentDescriptionFormat {
					t.Fatalf("expected description-format, got %+v", options.TsNoCheck)
				}
				if options.TsNoCheck.DescriptionFormat == nil ||
					options.TsNoCheck.DescriptionFormat.Source() != "^: TS\\d+$" {
					t.Fatalf("pattern did not survive the decode: %+v", options.TsNoCheck)
				}
			}},
		{"{\"minimumDescriptionLength\":21}", func(t *testing.T, options BanTsCommentOptions) {
			if options.MinimumDescriptionLength != 21 {
				t.Fatalf("expected 21, got %d", options.MinimumDescriptionLength)
			}
		}},
		{"{}", func(t *testing.T, options BanTsCommentOptions) {
			if options != defaults {
				t.Fatalf("an empty object should leave every default, got %+v", options)
			}
		}},
		// A pattern only JavaScript compiles: Go's regexp has no lookahead, and ESLint accepts it.
		{"{\"ts-ignore\":{\"descriptionFormat\":\"^(?=: TS)\"}}",
			func(t *testing.T, options BanTsCommentOptions) {
				if options.TsIgnore.DescriptionFormat == nil || options.TsIgnore.DescriptionFormat.Source() != "^(?=: TS)" {
					t.Fatalf("a lookahead pattern did not survive the decode: %+v", options.TsIgnore)
				}
			}},
	}
	for index, testCase := range cases {
		t.Run(banTsCommentCaseName(index), func(t *testing.T) {
			t.Parallel()
			decoded, err := DecodeBanTsCommentOptions([]byte(testCase.configuration))
			if err != nil {
				t.Fatalf("could not decode %s: %v", testCase.configuration, err)
			}
			options, isOptions := decoded.(BanTsCommentOptions)
			if !isOptions {
				t.Fatalf("decoder returned %T rather than BanTsCommentOptions", decoded)
			}
			testCase.check(t, options)
		})
	}
}

// TestDecodeBanTsCommentOptionsRefusesWhatUpstreamRefuses pins the refusals that replaced the decoder's
// fallback to the default (ruled on #pd2chkx): each value here fails typescript-eslint's schema or its
// `new RegExp`, so ESLint refuses the config, and so does cohere, naming the key.
func TestDecodeBanTsCommentOptionsRefusesWhatUpstreamRefuses(t *testing.T) {
	t.Parallel()

	cases := []struct {
		configuration string
		want          string
	}{
		{`{"ts-ignore":"nonsense"}`, `ts-ignore: expected true, false, "allow-with-description"`},
		{`{"ts-check":"Allow-with-description"}`, `ts-check: expected true, false, "allow-with-description"`},
		{`{"ts-nocheck":3}`, `ts-nocheck: expected true, false, "allow-with-description"`},
		{`{"ts-expect-error":[]}`, `ts-expect-error: expected true, false, "allow-with-description"`},
		{`{"ts-ignore":{"descriptionFormat":"^(unclosed"}}`, `ts-ignore: descriptionFormat "^(unclosed" is not a pattern JavaScript can compile`},
	}
	for index, testCase := range cases {
		t.Run(banTsCommentCaseName(index), func(t *testing.T) {
			t.Parallel()
			_, err := DecodeBanTsCommentOptions([]byte(testCase.configuration))
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("decoding %s: error %v, want one containing %q", testCase.configuration, err, testCase.want)
			}
		})
	}
}

// TestBanTsCommentDeclinesJavaScriptFiles pins upstream's should_run.
//
// A directive in a JavaScript file is how that file opts into checking, so upstream's
// `source_type().is_typescript()` gate is a decision rather than an optimization. Dropping it would
// report every `@ts-ignore` in every `.js` file on the tree, a false-positive class no imported
// fixture can see because the corpus is all TypeScript.
func TestBanTsCommentDeclinesJavaScriptFiles(t *testing.T) {
	t.Parallel()

	const source = "// @ts-ignore\n"

	for _, fileName := range []string{
		"/repository/source/Thing.js",
		"/repository/source/Thing.jsx",
		"/repository/source/Thing.mjs",
	} {
		t.Run(fileName, func(t *testing.T) {
			t.Parallel()
			rule_testing.ExpectClean(t, rule_testing.RunWithOptions(t, BanTsComment, fileName, source, nil))
		})
	}

	for _, fileName := range []string{
		"/repository/source/Thing.ts",
		"/repository/source/Thing.tsx",
		"/repository/source/Thing.mts",
		"/repository/source/Thing.cts",
	} {
		t.Run(fileName, func(t *testing.T) {
			t.Parallel()
			result := rule_testing.RunWithOptions(t, BanTsComment, fileName, source, nil)
			rule_testing.ExpectFindings(t, result, "banTsCommentPreferExpectError")
		})
	}
}

// TestBanTsCommentMessageTextNamesTheDirective asserts the rendered text, exactly.
//
// Three of the four messages interpolate, and ExpectFindings sees only the id, so a format string
// that dropped the directive name or doubled a character would leave every fixture above green.
//
// Two things about how this is written. The comparison is EQUALITY on the whole description rather
// than a Contains on a fragment, because a predicate weaker than the property it guards is not a
// guard: a doubled character produces a string that still contains the needle. And every expected
// string is a literal typed here rather than a call to the rule's own message constructor, because
// comparing a finding against the constant it was reported with is equality between two values that
// move together under mutation and therefore proves nothing.
func TestBanTsCommentMessageTextNamesTheDirective(t *testing.T) {
	t.Parallel()

	cases := []struct {
		configuration string
		sourceText    string
		wantId        string
		wantText      string
	}{
		{"", "// @ts-nocheck\n", "banTsComment",
			"This file silences the TypeScript compiler with `@ts-nocheck`. A directive comment " +
				"switches off checking for a line or a whole file, so the error it hides stays in " +
				"the code and stops being reported to anyone. Fix the underlying type error " +
				"instead, or narrow the suppression to `@ts-expect-error` with a description " +
				"saying why it is necessary."},
		{"{\"ts-check\":true}", "// @ts-check\n", "banTsComment",
			"This file silences the TypeScript compiler with `@ts-check`. A directive comment " +
				"switches off checking for a line or a whole file, so the error it hides stays in " +
				"the code and stops being reported to anyone. Fix the underlying type error " +
				"instead, or narrow the suppression to `@ts-expect-error` with a description " +
				"saying why it is necessary."},
		{"", "// @ts-ignore\n", "banTsCommentPreferExpectError",
			"This suppression uses `@ts-ignore`, which keeps silencing the line even after the " +
				"underlying error is gone, so a stale suppression is invisible forever. " +
				"`@ts-expect-error` reports when the line it guards has stopped failing, which is " +
				"what makes the suppression removable."},
		{"", "// @ts-expect-error\n", "banTsCommentRequiresDescription",
			"This `@ts-expect-error` carries no explanation, so nobody reading it later can tell " +
				"whether the suppression is still needed or what it was hiding. Write at least 3 " +
				"characters after the directive saying which error it suppresses and why the " +
				"error cannot be fixed."},
		{"{\"ts-nocheck\":\"allow-with-description\",\"minimumDescriptionLength\":21}",
			"// @ts-nocheck ab\n", "banTsCommentRequiresDescription",
			"This `@ts-nocheck` carries no explanation, so nobody reading it later can tell " +
				"whether the suppression is still needed or what it was hiding. Write at least 21 " +
				"characters after the directive saying which error it suppresses and why the " +
				"error cannot be fixed."},
		{"{\"ts-ignore\":{\"descriptionFormat\":\"^: TS[0-9]+$\"}}", "// @ts-ignore: nope\n",
			"banTsCommentDescriptionFormat",
			"The explanation after this `@ts-ignore` does not match the shape this project " +
				"requires, `^: TS[0-9]+$`. The format exists so a suppression can be traced back " +
				"to the error it hides, which a free-form note cannot be."},
	}
	for index, testCase := range cases {
		t.Run(banTsCommentCaseName(index), func(t *testing.T) {
			t.Parallel()
			result := rule_testing.RunWithOptions(t, BanTsComment, banTsCommentFile,
				testCase.sourceText, decodeBanTsCommentOptions(t, testCase.configuration))
			if len(result.Diagnostics) != 1 {
				t.Fatalf("expected one finding, got %d: %v", len(result.Diagnostics),
					result.MessageIds())
			}
			if result.Diagnostics[0].Message.Id != testCase.wantId {
				t.Fatalf("expected id %q, got %q", testCase.wantId,
					result.Diagnostics[0].Message.Id)
			}
			if result.Diagnostics[0].Message.Description != testCase.wantText {
				t.Fatalf("message text was\n  %q\nexpected\n  %q",
					result.Diagnostics[0].Message.Description, testCase.wantText)
			}
		})
	}
}

// TestBanTsCommentSurvivesAnUnterminatedBlockComment pins the guards in commentContent.
//
// Those guards are crash protection rather than behavioral filters, which is why the sweep reports
// them as survivors under any assertion about findings: no ExpectFindings fixture can see a panic.
// Ask what the line PREVENTS rather than what it decides, and this is what it prevents.
//
// The reachable input is error recovery. A well-formed block comment is at least four bytes
// (`/**/`), so a naive read says the four-byte guard is dead. Probed against the parser instead: an
// unterminated `/*` at the end of a file comes back as a block comment of length TWO, and `/*/` and
// `/**` come back as length three. Subtracting two from each end of a two-byte comment produces a
// range whose start is past its end, and slicing the source with it panics. Measured by
// neutralizing both guards, which turns every row below into a stack trace.
//
// A file ending in an unterminated comment is not exotic. It is what a half-written file on disk
// looks like while somebody is typing, and the linter runs on those.
func TestBanTsCommentSurvivesAnUnterminatedBlockComment(t *testing.T) {
	t.Parallel()

	for _, sourceText := range []string{"/*", "/*/", "/**", "/*@ts-ignore", "//", "/*@ts-ignore\n"} {
		t.Run(sourceText, func(t *testing.T) {
			t.Parallel()
			// The assertion is that this returns at all. A finding would also be acceptable for
			// some of these; a panic is not, and a panic is what the guards prevent.
			_ = rule_testing.RunWithOptions(t, BanTsComment, banTsCommentFile, sourceText, nil)
		})
	}
}
