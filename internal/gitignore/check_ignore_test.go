package gitignore

import (
	"strings"
	"testing"
)

// What git check-ignore answers, in its verbose form, for the precedence and scoping rules gitignore(5)
// states: the deciding `file:line:pattern` for each path, or `::` when no rule decides. The cases are ours,
// written from gitignore(5), and each one's verdict agrees with git 2.54's `check-ignore --no-index -v -n`
// over the same tree. Every ignore file opens with a comment or a blank line, so a line number that skipped
// them would name the wrong line.
func TestCheckIgnoreNamesTheDecidingRule(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	writeTree(test, root, map[string]string{
		".git/info/exclude": strings.Join([]string{
			"# the repository's own excludes",
			"scratch",
			"shared.txt",
			"!rescued.log",
			"tmp-*",
			"!tmp-keep",
			"override-me",
		}, "\n") + "\n",
		".gitignore": strings.Join([]string{
			"# build output",
			"",
			"*.log",
			"!important.log",
			"build/",
			"/top-only",
			"",
			"# a negation below an excluded directory cannot re-include",
			"excluded/",
			"!excluded/keep",
			"!shared.txt",
		}, "\n") + "\n",
		"a/.gitignore": strings.Join([]string{
			"",
			"/x",
			"# overrides the root's *.log for this directory down",
			"!*.log",
			"!override-me",
		}, "\n") + "\n",
		"a/b/.gitignore": "# and this one overrides a's negation again\n*.log\n",
		"build/":         "",
		"a/build/":       "",
		"a/b/build":      "",
		"excluded/":      "",
		"excluded/keep":  "",
	})
	matcher, newGitignoreError := New(root)
	if newGitignoreError != nil {
		test.Fatal(newGitignoreError)
	}

	cases := []struct {
		path        string
		directory   bool
		wantIgnored bool
		wantVerbose string
	}{
		// A negation re-includes what an earlier line excluded.
		{"debug.log", false, true, ".gitignore:3:*.log"},
		{"important.log", false, false, ".gitignore:4:!important.log"},
		// Nothing below an excluded directory is re-included: git never reads inside it.
		{"excluded", true, true, ".gitignore:9:excluded/"},
		{"excluded/keep", false, true, ".gitignore:9:excluded/"},

		// A trailing `/` matches a directory, at any depth, and never a file of that name.
		{"build", true, true, ".gitignore:5:build/"},
		{"build", false, false, "::"},
		{"a/build", true, true, ".gitignore:5:build/"},
		{"a/b/build", false, false, "::"},

		// A leading `/` anchors a pattern to its own file's directory.
		{"top-only", false, true, ".gitignore:6:/top-only"},
		{"a/top-only", false, false, "::"},
		{"a/x", false, true, "a/.gitignore:2:/x"},
		{"x", false, false, "::"},
		{"a/b/x", false, false, "::"},

		// A deeper file decides before a shallower one, whichever way it goes.
		{"a/debug.log", false, false, "a/.gitignore:4:!*.log"},
		{"a/b/debug.log", false, true, "a/b/.gitignore:2:*.log"},

		// info/exclude decides only what no .gitignore does, the last matching line within it first.
		{"scratch", false, true, ".git/info/exclude:2:scratch"},
		{"a/b/scratch", false, true, ".git/info/exclude:2:scratch"},
		{"tmp-1", false, true, ".git/info/exclude:5:tmp-*"},
		{"tmp-keep", false, false, ".git/info/exclude:6:!tmp-keep"},
		{"shared.txt", false, false, ".gitignore:11:!shared.txt"},
		{"rescued.log", false, true, ".gitignore:3:*.log"},
		{"override-me", false, true, ".git/info/exclude:7:override-me"},
		{"a/override-me", false, false, "a/.gitignore:5:!override-me"},
	}
	for _, testCase := range cases {
		ignored, decided := decide(test, matcher, testCase.path, testCase.directory)
		gotVerbose := "::"
		if !decided.IsZero() {
			gotVerbose = decided.String()
		}
		if ignored != testCase.wantIgnored || gotVerbose != testCase.wantVerbose {
			test.Errorf("%s (directory %v): got %v by %s, want %v by %s",
				testCase.path, testCase.directory, ignored, gotVerbose, testCase.wantIgnored, testCase.wantVerbose)
		}
	}
}
