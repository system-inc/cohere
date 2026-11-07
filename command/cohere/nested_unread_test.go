//go:build unix

// Run through runForJson, which unloaded_project_test.go defines for Unix alone.

package main

import (
	"strings"
	"testing"
)

// A nested repository that can't be read stops nothing: the project's own types and lint run, and the summary
// and the footer name the repository as a gap (#71njcj8). At adamic's root a nested cohere/ checkout the format
// walk could not read stopped the run inside the fix phase, and its summary read no phases and 0 rules over 706
// files a tsconfig run found 801 findings in. Here the nested repository holds a .prettierignore, which the walk
// refuses, and the root's file holds a debugger statement lint must still find.
func TestANestedRepositoryThatCannotBeReadStopsNothing(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := newRepository(test, map[string]string{
		"tsconfig.json":              `{"compilerOptions":{"strict":true,"noEmit":true},"include":["source"]}`,
		"CohereSettings.json":        `{"rules":{"no-debugger":"error"}}`,
		"source/a.ts":                "export function value(): number {\n  debugger;\n  return 1;\n}\n",
		".gitmodules":                "[submodule \"vendor/lib\"]\n\tpath = vendor/lib\n\turl = ./vendor/lib\n",
		"vendor/lib/.git/HEAD":       "ref: refs/heads/main\n",
		"vendor/lib/data.json":       "{ \"a\": 1 }\n",
		"vendor/lib/.prettierignore": "dist\n",
	})

	stdout, _, exitCode := runForJson(test, binary, root, "--no-fix")
	summary, found := summariesOf(test, stdout)[""]
	if !found || exitCode == 0 {
		test.Fatalf("want one failing summary, got exit %d:\n%s", exitCode, stdout)
	}
	if !strings.Contains(stdout, `"rule":"no-debugger"`) {
		test.Errorf("lint did not run past the nested repository it could not read: %v", summary)
	}
	if unread := summary.Gaps.NestedUnread; !strings.Contains(unread, "vendor/lib") ||
		!strings.Contains(unread, ".prettierignore") {
		test.Errorf("the summary does not name the nested repository it could not read: %+v", summary.Gaps)
	}
	if summary.Error != "" {
		test.Errorf("the run stopped on the nested repository: %s", summary.Error)
	}

	output, _ := runWithEngine(test, binary, "", root, "--no-fix")
	if !strings.Contains(output, "⚠ nested repositories not read: ") {
		test.Errorf("the footer does not name the gap:\n%s", output)
	}
}
