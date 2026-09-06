package nexus

import (
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/rule_testing"
)

const shoutingFile = "/repository/source/Thing.ts"

func TestConsistencyNoShoutingFires(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		sourceText string
	}{
		{"shouted word in a line comment", "// NEVER cache this\nexport const Value = 1;\n"},
		{"shouted word in a block comment", "/* this is REALLY important */\nexport const Value = 1;\n"},
		{"banner line", "// DO NOT EDIT\nexport const Value = 1;\n"},
		{"shouted two-letter word", "// this IS the one\nexport const Value = 1;\n"},
		{"shouted word in jsdoc", "/** ALWAYS returns a copy */\nexport const Value = 1;\n"},
		// An unterminated backtick must not mask the rest of the comment. If it did, opening a
		// backtick would be a way to turn the rule off for everything after it.
		{"unclosed backtick does not mask the rest", "// `code and then NEVER do this\nexport const Value = 1;\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := rule_testing.Run(t, ConsistencyNoShouting, shoutingFile, testCase.sourceText)
			rule_testing.ExpectFindings(t, result, "shoutingInComment")
		})
	}
}

func TestConsistencyNoShoutingStaysSilent(t *testing.T) {
	t.Parallel()

	// These are the discriminating cases. A rule that only detects capitals would fire on every
	// one of them, and a rule that fires on ordinary technical prose is a rule people disable.
	cases := []struct {
		name       string
		sourceText string
	}{
		{"ordinary prose", "// parse the body before validating it\nexport const Value = 1;\n"},
		{"allowlisted acronyms", "// the JSON body arrives over HTTP from the API\nexport const Value = 1;\n"},
		{"consonant cluster acronym", "// send it over RPC to the DB\nexport const Value = 1;\n"},
		{"backticked identifier", "// the `NOT_FOUND` branch returns early\nexport const Value = 1;\n"},
		{"backticked shouted word", "// the `NEVER` constant is a sentinel\nexport const Value = 1;\n"},
		{"identifier with underscore", "// read DATABASE_URL from the environment\nexport const Value = 1;\n"},
		{"identifier with a digit", "// the H3 heading and the C1 column\nexport const Value = 1;\n"},
		{"currency code", "// amounts in KRW have no minor unit\nexport const Value = 1;\n"},
		{"double quoted literal", "// \"ORDER STATUS\" is the column label\nexport const Value = 1;\n"},
		{"single quoted token", "// pass 'US' as the region\nexport const Value = 1;\n"},
		{"fenced block", "/*\n * ```\n * NEVER DO THIS\n * ```\n */\nexport const Value = 1;\n"},
		{"command line", "/*\n * git commit --amend NEVER\n */\nexport const Value = 1;\n"},
		{"two-letter abbreviation", "// the IP address and the OS version\nexport const Value = 1;\n"},
		{"notation shorthand", "// TODO: handle the empty case\nexport const Value = 1;\n"},
		{"no comments at all", "export const Value = 1;\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := rule_testing.Run(t, ConsistencyNoShouting, shoutingFile, testCase.sourceText)
			rule_testing.ExpectClean(t, result)
		})
	}
}

func TestConsistencyNoShoutingRespectsTheAllowOption(t *testing.T) {
	t.Parallel()

	sourceText := "// the WIDGET subsystem owns this\nexport const Value = 1;\n"

	withoutOption := rule_testing.Run(t, ConsistencyNoShouting, shoutingFile, sourceText)
	rule_testing.ExpectFindings(t, withoutOption, "shoutingInComment")

	withOption := rule_testing.RunWithOptions(t, ConsistencyNoShouting, shoutingFile, sourceText,
		ConsistencyNoShoutingOptions{Allow: []string{"WIDGET"}})
	rule_testing.ExpectClean(t, withOption)
}

// The message has to name what it saw. A finding that says only "this comment shouts" makes the
// reader rescan a paragraph to find the word that tripped it.
func TestConsistencyNoShoutingNamesTheTokens(t *testing.T) {
	t.Parallel()

	result := rule_testing.Run(t, ConsistencyNoShouting, shoutingFile,
		"// NEVER cache this, it is REALLY bad\nexport const Value = 1;\n")
	rule_testing.ExpectFindings(t, result, "shoutingInComment")

	description := result.Diagnostics[0].Message.Description
	for _, want := range []string{`"NEVER"`, `"REALLY"`} {
		if !strings.Contains(description, want) {
			t.Fatalf("expected the message to name %s, got: %s", want, description)
		}
	}
}
