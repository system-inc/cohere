package nexus

import (
	"testing"

	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/testing"
)

// separatedFolderWordsAtProject is the rule's options as a run rooted at `/project` decodes them.
var separatedFolderWordsAtProject = ProjectRootOptions{ProjectRoot: "/project"}

// Each flagged file is reported once, at its start, naming its outermost folder that runs words together and the
// name it asks for: a hyphen where the folder separates no words yet, and the folder's own separator where it does.
func TestConsistencyRequireSeparatedFolderWordsFires(test *testing.T) {
	test.Parallel()

	cases := []struct {
		fileName    string
		folder      string
		replacement string
	}{
		{"/project/source/formatoptions/Thing.ts", "formatoptions", "format-options"},
		{"/project/goengine/Thing.ts", "goengine", "go-engine"},
		{"/project/source/profileimages-leaf/Thing.tsx", "profileimages-leaf", "profile-images-leaf"},
		{"/project/source/text_formatoptions/Thing.ts", "text_formatoptions", "text_format_options"},
		{"/project/source/imessagebridge/Thing.ts", "imessagebridge", "imessage-bridge"},
		{"/project/source/serviceTypes/Thing.ts", "serviceTypes", "service-types"},
		// A private folder's name after its underscore is the project's own.
		{"/project/app/_formatoptions/Thing.tsx", "_formatoptions", "_format-options"},
		// A folder inside an App Router folder is still the project's.
		{"/project/app/[id]/networth/page.tsx", "networth", "net-worth"},
		// Two folders that run together are one finding, for the outermost.
		{"/project/formatoptions/jsonlines/Thing.ts", "formatoptions", "format-options"},
	}
	for _, testCase := range cases {
		test.Run(testCase.fileName, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunWithOptions(
				test,
				ConsistencyRequireSeparatedFolderWords,
				testCase.fileName,
				"export const Value = 1;\n",
				separatedFolderWordsAtProject,
			)
			rule_testing.ExpectFindings(test, result, "runTogetherProjectFolder")
			want := `Folder name "` + testCase.folder + `" runs its words together. Use "` + testCase.replacement +
				"\" instead. A mind reading cold reads `format-options` at a glance and has to stop and parse " +
				"`formatoptions`, and a folder's name is read on every import line and every path, so its words " +
				"are separated."
			if got := result.Diagnostics[0].Message.Description; got != want {
				test.Errorf("message = %q, want %q", got, want)
			}
			if position := result.Diagnostics[0].Range; position.Pos() != 0 || position.End() != 0 {
				test.Errorf("finding at %d to %d, want zero width at the file's start", position.Pos(), position.End())
			}
		})
	}
}

// Words, separated names, names the lists cannot read, App Router folders and a file's own name all pass.
func TestConsistencyRequireSeparatedFolderWordsStaysSilent(test *testing.T) {
	test.Parallel()

	cases := []string{
		"/project/source/workspace/Thing.ts",
		"/project/source/changelog/Thing.ts",
		"/project/source/format-options/Thing.ts",
		"/project/source/format_options/Thing.ts",
		"/project/source/net-worth/by-payee/Thing.ts",
		"/project/source/cachedvfs/Thing.ts",
		"/project/source/social-media-v2/Thing.ts",
		"/project/app/_components/Thing.tsx",
		// App Router folders, told by their punctuation. Each holds a part that runs together between two
		// separators, where no punctuation stops the split, so only the routing test keeps it clean.
		"/project/app/(main-profileimages-layout)/page.tsx",
		"/project/app/[...slug-profileimages-path]/page.tsx",
		"/project/app/@modal-profileimages-slot/page.tsx",
		"/project/app/(..)photo-profileimages-view/page.tsx",
		"/project/app/(oslayout)/page.tsx",
		"/project/app/[entityKey]/page.tsx",
		"/project/app/[...notfound]/page.tsx",
		"/project/app/[[...slugpath]]/page.tsx",
		"/project/app/@modalslot/page.tsx",
		"/project/app/(..)photoview/page.tsx",
		// A file's own name is not a folder.
		"/project/source/FormatOptions.ts",
		"/project/formatoptions.ts",
	}
	for _, fileName := range cases {
		test.Run(fileName, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunWithOptions(
				test,
				ConsistencyRequireSeparatedFolderWords,
				fileName,
				"export const Value = 1;\n",
				separatedFolderWordsAtProject,
			)
			rule_testing.ExpectClean(test, result)
		})
	}
}

// Only the project's own folders are judged: not the checkout's name, not a file outside the root, and nothing
// below node_modules, even under a folder of the project's that runs together.
func TestConsistencyRequireSeparatedFolderWordsJudgesOnlyTheProjectsFolders(test *testing.T) {
	test.Parallel()

	cases := []struct {
		projectRoot string
		fileName    string
	}{
		{"/checkouts/goengine", "/checkouts/goengine/source/Thing.ts"},
		{"/checkouts/goengine/", "/checkouts/goengine/Thing.ts"},
		{"/project", "/elsewhere/formatoptions/Thing.ts"},
		// A sibling whose name starts with the root's is not inside it.
		{"/project", "/projectformatoptions/source/Thing.ts"},
		{"/project", "/project/node_modules/somepackage/formatoptions/index.ts"},
		{"/project", "/project/formatoptions/node_modules/somepackage/index.ts"},
	}
	for _, testCase := range cases {
		test.Run(testCase.fileName, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunWithOptions(
				test,
				ConsistencyRequireSeparatedFolderWords,
				testCase.fileName,
				"export const Value = 1;\n",
				ProjectRootOptions{ProjectRoot: testCase.projectRoot},
			)
			rule_testing.ExpectClean(test, result)
		})
	}

	inside := rule_testing.RunWithOptions(
		test,
		ConsistencyRequireSeparatedFolderWords,
		"/checkouts/goengine/source/formatoptions/Thing.ts",
		"export const Value = 1;\n",
		ProjectRootOptions{ProjectRoot: "/checkouts/goengine"},
	)
	rule_testing.ExpectFindings(test, inside, "runTogetherProjectFolder")
}

// With no project root, nothing is judged, rather than the whole path judged against a guess. Run bound, so the
// docs capture never shows a file in a run-together folder as a clean example.
func TestConsistencyRequireSeparatedFolderWordsJudgesNothingWithoutARoot(test *testing.T) {
	test.Parallel()

	unconfigured := rule.Bind(ConsistencyRequireSeparatedFolderWords, rule.Given[ProjectRootOptions]{})
	result := rule_testing.RunBound(test, unconfigured, "/project/formatoptions/Thing.ts", "export const Value = 1;\n")
	rule_testing.ExpectClean(test, result)
}

// The registration's decoder takes the root a run discovered for a bare severity, and refuses any option element
// and a base that knows no root.
func TestConsistencyRequireSeparatedFolderWordsDecodesTheRunsRoot(test *testing.T) {
	test.Parallel()

	var entry rule.Entry
	for _, registered := range rule.Registered() {
		if registered.Name == ConsistencyRequireSeparatedFolderWords.Name {
			entry = registered
		}
	}
	if entry.Bind == nil {
		test.Fatalf("%s is not registered with a decoder", ConsistencyRequireSeparatedFolderWords.Name)
	}
	bound, bindError := entry.Bind(nil, rule.OptionsBase{ProjectRoot: "/checkouts/goengine"})
	if bindError != nil {
		test.Fatalf("binding at a root: %v", bindError)
	}
	inside := rule_testing.RunBound(test, bound, "/checkouts/goengine/jsonlines/Thing.ts", "export const Value = 1;\n")
	rule_testing.ExpectFindings(test, inside, "runTogetherProjectFolder")
	atRoot := rule_testing.RunBound(test, bound, "/checkouts/goengine/source/Thing.ts", "export const Value = 1;\n")
	rule_testing.ExpectClean(test, atRoot)

	if _, elementError := entry.Bind([]byte(`{}`), rule.OptionsBase{ProjectRoot: "/project"}); elementError == nil {
		test.Error("an option element decoded, and the rule takes none")
	}
	if _, rootError := entry.Bind(nil, rule.OptionsBase{}); rootError == nil {
		test.Error("a base with no project root decoded, and the rule would judge against a guess")
	}
}
