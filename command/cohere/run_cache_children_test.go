package main

import (
	"testing"
)

// TestTheRunCacheJudgesADiscoveryChildByItsParentsCommandLine: one of discovery's children is admitted by the command
// line its parent was given, the `--directory` and `--tsconfig` runProject prepends and the absolute `--lint-config`
// childArguments appends taken off in exactly those places. The same flags typed by a person, or found anywhere
// else in a child's command line, keep the run out, so the runs that replay are only the ones runCacheEligible names.
func TestTheRunCacheJudgesADiscoveryChildByItsParentsCommandLine(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name      string
		arguments []string
		child     bool
		admitted  bool
	}{
		{"a child of a bare --no-fix", []string{"--directory", "/work/service", "--no-fix"}, true, true},
		{"a child with its tsconfig", []string{"--directory", "/work/web", "--tsconfig", "tsconfig.app.json",
			"--no-fix", "--verbose"}, true, true},
		{"a child with the parent's lint config", []string{"--directory", "/work/web", "--no-fix", "--lint-config",
			"/work/Settings.json"}, true, true},
		{"a child of a bare run", []string{"--directory", "/work/web"}, true, true},
		{"--directory typed by a person", []string{"--directory", "/work/service", "--no-fix"}, false, false},
		{"a relative lint config in a child", []string{"--directory", "/work/web", "--lint-config",
			"Settings.json"}, true, false},
		{"--directory not where the parent puts it", []string{"--no-fix", "--directory", "/work/web"}, true, false},
		{"a tsconfig with no directory before it", []string{"--tsconfig", "tsconfig.json", "--no-fix"}, true, false},
		{"a second --directory the parent was given", []string{"--directory", "/work/web", "--directory", "/x"},
			true, false},
		{"a child of a run with a flag the cache does not serve", []string{"--directory", "/work/web", "--timing"},
			true, false},
	}
	for _, testCase := range cases {
		admitted := runCacheAdmits(runCacheArguments(testCase.arguments, testCase.child))
		if admitted != testCase.admitted {
			test.Errorf("%s: admitted %t, want %t", testCase.name, admitted, testCase.admitted)
		}
	}
}
