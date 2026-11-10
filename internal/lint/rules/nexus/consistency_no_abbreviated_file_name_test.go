package nexus

import (
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

// abbreviatedFileNameReasoning is the sentence both of the rule's messages end on, the `reasoning` phrase of its
// message file, written out here so a rendered text is compared whole.
const abbreviatedFileNameReasoning = "A file's name is read on every path and every import, so the letters saved " +
	"when it was named are paid back by every reader who has to expand the word themselves and hope they expanded " +
	"it the way the author meant."

func TestConsistencyNoAbbreviatedFileNameFires(test *testing.T) {
	test.Parallel()

	cases := []struct {
		fileName string
		wantId   string
		message  string
	}{
		{
			"/project/source/cfg.ts",
			"abbreviatedFileName",
			`File name "cfg.ts" abbreviates "cfg". Spell it out as "configuration".`,
		},
		{
			"/project/source/msgPrinter.ts",
			"abbreviatedFileName",
			`File name "msgPrinter.ts" abbreviates "msg". Spell it out as "message".`,
		},
		{
			"/project/source/MsgPrinter.tsx",
			"abbreviatedFileName",
			`File name "MsgPrinter.tsx" abbreviates "msg". Spell it out as "message".`,
		},
		{
			"/project/source/msg-printer.ts",
			"abbreviatedFileName",
			`File name "msg-printer.ts" abbreviates "msg". Spell it out as "message".`,
		},
		// The test marker is set aside, so the word the name ends on is the name's own.
		{
			"/project/source/errorMsg.test.ts",
			"abbreviatedFileName",
			`File name "errorMsg.test.ts" abbreviates "msg". Spell it out as "message".`,
		},
		{
			"/project/source/posLabel.ts",
			"abbreviatedFileName",
			`File name "posLabel.ts" abbreviates "pos". Spell it out as "position" or "positive", whichever it means.`,
		},
		{
			"/project/source/fn.ts",
			"abbreviatedFileNameAdvice",
			`File name "fn.ts" abbreviates "fn". Use "callback", "handler", "factory", or a more descriptive name.`,
		},
	}
	for _, testCase := range cases {
		test.Run(testCase.fileName, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunWithOptions(
				test,
				ConsistencyNoAbbreviatedFileName,
				testCase.fileName,
				"export const Value = 1;\n",
				consistencyNoFileNameAtProject,
			)
			rule_testing.ExpectFindings(test, result, testCase.wantId)
			want := testCase.message + " " + abbreviatedFileNameReasoning
			if got := result.Diagnostics[0].Message.Description; got != want {
				test.Errorf("message = %q, want %q", got, want)
			}
			if position := result.Diagnostics[0].Range; position.Pos() != 0 || position.End() != 0 {
				test.Errorf("finding at %d to %d, want zero width at the file's start", position.Pos(), position.End())
			}
		})
	}
}

// Names spelled in full, the role markers a tool reads, and the files whose names are not the project's: Next.js's
// own, the one it writes, a generated file, and those under a folder of someone else's.
func TestConsistencyNoAbbreviatedFileNameStaysSilent(test *testing.T) {
	test.Parallel()

	cases := []string{
		"/project/source/messagePrinter.ts",
		"/project/source/configuration.ts",
		"/project/next.config.ts",
		"/project/vitest.config.mts",
		"/project/next-env.d.ts",
		"/project/app/page.tsx",
		"/project/app/opengraph-image2.tsx",
		"/project/source/MsgPrinter.generated.ts",
		"/project/node_modules/library/cfg.ts",
		"/project/source/testdata/cfg.ts",
	}
	for _, fileName := range cases {
		test.Run(fileName, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunWithOptions(
				test,
				ConsistencyNoAbbreviatedFileName,
				fileName,
				"export const Value = 1;\n",
				consistencyNoFileNameAtProject,
			)
			rule_testing.ExpectClean(test, result)
		})
	}
}
