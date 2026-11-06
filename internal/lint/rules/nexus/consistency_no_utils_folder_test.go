package nexus

import (
	"testing"

	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/testing"
)

// consistencyNoUtilsFolderAtProject is the rule's options as a run rooted at `/project` decodes them.
var consistencyNoUtilsFolderAtProject = ProjectRootOptions{ProjectRoot: "/project"}

func TestConsistencyNoUtilsFolderFires(test *testing.T) {
	test.Parallel()

	cases := []struct {
		fileName string
		wantId   string
		message  string
	}{
		{
			"/project/source/utils/Thing.ts",
			"noUtils",
			`Folder name "utils" is not allowed. Use "utilities" instead.`,
		},
		{
			"/project/source/_utils/Thing.ts",
			"noUnderscoreUtils",
			`Folder name "_utils" is not allowed. Use "_utilities" instead.`,
		},
		{
			"/project/utils/nested/deep/Thing.ts",
			"noUtils",
			`Folder name "utils" is not allowed. Use "utilities" instead.`,
		},
		{
			"/project/source/util/Thing.ts",
			"noUtils",
			`Folder name "util" is not allowed. Use "utilities" instead.`,
		},
		{
			"/project/source/Utils/Thing.ts",
			"noUtils",
			`Folder name "Utils" is not allowed. Use "Utilities" instead.`,
		},
		{
			"/project/source/_Utils/Thing.ts",
			"noUnderscoreUtils",
			`Folder name "_Utils" is not allowed. Use "_Utilities" instead.`,
		},
		{
			"/project/source/__util/Thing.ts",
			"noUnderscoreUtils",
			`Folder name "__util" is not allowed. Use "__utilities" instead.`,
		},
		{
			"/project/source/UTILS/Thing.ts",
			"noUtils",
			`Folder name "UTILS" is not allowed. Use "UTILITIES" instead.`,
		},
		// Two banned folders are one finding, for the outermost, since the file is what is misplaced.
		{
			"/project/utils/_utils/Thing.ts",
			"noUtils",
			`Folder name "utils" is not allowed. Use "utilities" instead.`,
		},
	}
	for _, testCase := range cases {
		test.Run(testCase.fileName, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunWithOptions(
				test,
				ConsistencyNoUtilsFolder,
				testCase.fileName,
				"export const Value = 1;\n",
				consistencyNoUtilsFolderAtProject,
			)
			rule_testing.ExpectFindings(test, result, testCase.wantId)
			if got := result.Diagnostics[0].Message.Description; got != testCase.message {
				test.Errorf("message = %q, want %q", got, testCase.message)
			}
			if position := result.Diagnostics[0].Range; position.Pos() != 0 || position.End() != 0 {
				test.Errorf("finding at %d to %d, want zero width at the file's start", position.Pos(), position.End())
			}
		})
	}
}

func TestConsistencyNoUtilsFolderStaysSilent(test *testing.T) {
	test.Parallel()

	// A substring match would flag the last three, and a rule that fires on the exact spelling it is
	// asking for is worse than no rule at all.
	cases := []string{
		"/project/source/utilities/Thing.ts",
		"/project/source/_utilities/Thing.ts",
		"/project/source/components/Thing.ts",
		"/project/source/utilsomething/Thing.ts",
		"/project/source/myutils/Thing.ts",
		"/project/source/utility/Thing.ts",
		// A file's own name is not a folder.
		"/project/source/utils.ts",
		"/project/utils.ts",
	}
	for _, fileName := range cases {
		test.Run(fileName, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunWithOptions(
				test,
				ConsistencyNoUtilsFolder,
				fileName,
				"export const Value = 1;\n",
				consistencyNoUtilsFolderAtProject,
			)
			rule_testing.ExpectClean(test, result)
		})
	}
}

// Only the folders below the project root are judged. A checkout under a directory named `utils` is
// not something anybody inside the project can rename (uf06), and a file outside the root is not the
// project's to move.
func TestConsistencyNoUtilsFolderJudgesOnlyInsideTheProject(test *testing.T) {
	test.Parallel()

	cases := []struct {
		projectRoot string
		fileName    string
	}{
		{"/checkouts/utils/project", "/checkouts/utils/project/source/Thing.ts"},
		{"/checkouts/_Util/project/", "/checkouts/_Util/project/source/Thing.ts"},
		{"/project", "/elsewhere/utils/Thing.ts"},
		// A sibling whose name starts with the root's is not inside it.
		{"/project", "/project-utils/utils/Thing.ts"},
	}
	for _, testCase := range cases {
		test.Run(testCase.fileName, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunWithOptions(
				test,
				ConsistencyNoUtilsFolder,
				testCase.fileName,
				"export const Value = 1;\n",
				ProjectRootOptions{ProjectRoot: testCase.projectRoot},
			)
			rule_testing.ExpectClean(test, result)
		})
	}

	inside := rule_testing.RunWithOptions(
		test,
		ConsistencyNoUtilsFolder,
		"/checkouts/utils/project/source/utils/Thing.ts",
		"export const Value = 1;\n",
		ProjectRootOptions{ProjectRoot: "/checkouts/utils/project"},
	)
	rule_testing.ExpectFindings(test, inside, "noUtils")
}

// With no project root, nothing is judged, rather than the whole path judged against a guess. Run bound,
// so the docs capture never shows a file in a `utils` folder as a clean example.
func TestConsistencyNoUtilsFolderJudgesNothingWithoutARoot(test *testing.T) {
	test.Parallel()

	unconfigured := rule.Bind(ConsistencyNoUtilsFolder, rule.Given[ProjectRootOptions]{})
	result := rule_testing.RunBound(test, unconfigured, "/project/source/utils/Thing.ts", "export const Value = 1;\n")
	rule_testing.ExpectClean(test, result)
}

// The registration's decoder takes the root a run discovered for a bare severity, and refuses any option
// element and a base that knows no root.
func TestConsistencyNoUtilsFolderDecodesTheRunsRoot(test *testing.T) {
	test.Parallel()

	var entry rule.Entry
	for _, registered := range rule.Registered() {
		if registered.Name == ConsistencyNoUtilsFolder.Name {
			entry = registered
		}
	}
	if entry.Bind == nil {
		test.Fatalf("%s is not registered with a decoder", ConsistencyNoUtilsFolder.Name)
	}
	bound, bindError := entry.Bind(nil, rule.OptionsBase{ProjectRoot: "/checkouts/utils/project"})
	if bindError != nil {
		test.Fatalf("binding at a root: %v", bindError)
	}
	inside := rule_testing.RunBound(test, bound, "/checkouts/utils/project/utils/Thing.ts", "export const Value = 1;\n")
	rule_testing.ExpectFindings(test, inside, "noUtils")
	above := rule_testing.RunBound(test, bound, "/checkouts/utils/project/source/Thing.ts", "export const Value = 1;\n")
	rule_testing.ExpectClean(test, above)

	if _, elementError := entry.Bind([]byte(`{}`), rule.OptionsBase{ProjectRoot: "/project"}); elementError == nil {
		test.Error("an option element decoded, and the rule takes none")
	}
	if _, rootError := entry.Bind(nil, rule.OptionsBase{}); rootError == nil {
		test.Error("a base with no project root decoded, and the rule would judge against a guess")
	}
}
