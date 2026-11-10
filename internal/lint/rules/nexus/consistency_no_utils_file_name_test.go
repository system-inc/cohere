package nexus

import (
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

// consistencyNoFileNameAtProject is the file-name rules' options as a run rooted at `/project` decodes them.
var consistencyNoFileNameAtProject = ProjectRootOptions{ProjectRoot: "/project"}

func TestConsistencyNoUtilsFileNameFires(test *testing.T) {
	test.Parallel()

	cases := []struct {
		fileName string
		message  string
	}{
		{
			"/project/source/utils.ts",
			`File name "utils.ts" holds "utils", which names nothing in particular, so the file collects whatever ` +
				`has no other home. Name the file for what it holds, or at the least spell the word "utilities", as ` +
				`the house spells it in a folder's name.`,
		},
		{
			"/project/source/stringUtils.ts",
			`File name "stringUtils.ts" holds "Utils", which names nothing in particular, so the file collects ` +
				`whatever has no other home. Name the file for what it holds, or at the least spell the word ` +
				`"Utilities", as the house spells it in a folder's name.`,
		},
		{
			"/project/source/StringUtils.tsx",
			`File name "StringUtils.tsx" holds "Utils", which names nothing in particular, so the file collects ` +
				`whatever has no other home. Name the file for what it holds, or at the least spell the word ` +
				`"Utilities", as the house spells it in a folder's name.`,
		},
		{
			"/project/source/string-util.ts",
			`File name "string-util.ts" holds "util", which names nothing in particular, so the file collects ` +
				`whatever has no other home. Name the file for what it holds, or at the least spell the word ` +
				`"utilities", as the house spells it in a folder's name.`,
		},
		{
			"/project/source/_utils.ts",
			`File name "_utils.ts" holds "utils", which names nothing in particular, so the file collects whatever ` +
				`has no other home. Name the file for what it holds, or at the least spell the word "utilities", as ` +
				`the house spells it in a folder's name.`,
		},
		// A role marker is set aside, and the name before it still holds the word.
		{
			"/project/source/utils.test.ts",
			`File name "utils.test.ts" holds "utils", which names nothing in particular, so the file collects ` +
				`whatever has no other home. Name the file for what it holds, or at the least spell the word ` +
				`"utilities", as the house spells it in a folder's name.`,
		},
		// A dot that is no tool's marker hides nothing.
		{
			"/project/source/string.utils.ts",
			`File name "string.utils.ts" holds "utils", which names nothing in particular, so the file collects ` +
				`whatever has no other home. Name the file for what it holds, or at the least spell the word ` +
				`"utilities", as the house spells it in a folder's name.`,
		},
	}
	for _, testCase := range cases {
		test.Run(testCase.fileName, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunWithOptions(
				test,
				ConsistencyNoUtilsFileName,
				testCase.fileName,
				"export const Value = 1;\n",
				consistencyNoFileNameAtProject,
			)
			rule_testing.ExpectFindings(test, result, "utilsFileName")
			if got := result.Diagnostics[0].Message.Description; got != testCase.message {
				test.Errorf("message = %q, want %q", got, testCase.message)
			}
			if position := result.Diagnostics[0].Range; position.Pos() != 0 || position.End() != 0 {
				test.Errorf("finding at %d to %d, want zero width at the file's start", position.Pos(), position.End())
			}
		})
	}
}

// The near misses, and the files that are not the project's to name: generated, Next.js's own, under a folder of
// someone else's, or outside the project.
func TestConsistencyNoUtilsFileNameStaysSilent(test *testing.T) {
	test.Parallel()

	cases := []string{
		"/project/source/utilities.ts",
		"/project/source/stringFormatting.ts",
		"/project/source/myutils.ts",
		"/project/source/utility.ts",
		// A folder is the folder rule's.
		"/project/source/utils/Thing.ts",
		"/project/source/StringUtils.generated.ts",
		"/project/node_modules/library/utils.ts",
		"/project/vendor/utils.ts",
		"/project/third_party/library/utils.ts",
		"/project/source/testdata/utils.ts",
		"/elsewhere/utils.ts",
	}
	for _, fileName := range cases {
		test.Run(fileName, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunWithOptions(
				test,
				ConsistencyNoUtilsFileName,
				fileName,
				"export const Value = 1;\n",
				consistencyNoFileNameAtProject,
			)
			rule_testing.ExpectClean(test, result)
		})
	}
}
