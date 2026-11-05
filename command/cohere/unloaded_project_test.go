//go:build unix

// Run through runWithEngine, which projects_test.go defines for Unix alone (#tejf9bc).

package main

import (
	"strings"
	"testing"
)

// A project whose tsconfig cannot be read, inside a root whose tsconfig includes its files, is judged by
// its load failure and by nothing else (#0xg4y8w). At ahra's root, two untracked copies of a project whose
// Structure base was missing read 170,000 findings: the root linted their files in its own program, where
// their imports resolve to nothing, so every type-aware rule fired. The run fails naming the error with its
// text, the unloaded project's files carry no finding of any rule, and the root's own file is still judged.
func TestAProjectWhoseTsconfigFailsToLoadIsJudgedByThatFailureAlone(t *testing.T) {
	t.Parallel()
	binary := buildCohere(t)
	root := newRepository(t, map[string]string{
		"tsconfig.json": `{` + ownershipOptions + `,"include":["**/*.ts"]}`,
		"CohereSettings.json": `{"rules":{"no-debugger":"error","@typescript-eslint/no-unsafe-call":"error",` +
			`"@typescript-eslint/no-unsafe-member-access":"error"}}`,
		"index.ts":           "export function value(): number {\n  debugger;\n  return 1;\n}\n",
		"wire/tsconfig.json": `{"extends":"./libraries/structure/TypeScriptConfiguration.jsonc"}`,
		"wire/source/server.ts": "import { start } from \"absent-package\";\n\nexport function serve(): void {\n" +
			"  debugger;\n  start().listen.now();\n}\n",
	})
	output, exitCode := runWithEngine(t, binary, "", root, "--no-fix", "--no-format")
	if exitCode == 0 {
		t.Fatalf("a project whose tsconfig does not load passed the run:\n%s", output)
	}
	if !strings.Contains(output, "TS6053: File '") || !strings.Contains(output, "TypeScriptConfiguration.jsonc' not found") {
		t.Errorf("the run does not name the load failure with its text:\n%s", output)
	}
	if strings.Contains(output, "server.ts:") {
		t.Errorf("the unloaded project's file was judged by another project's program:\n%s", output)
	}
	if !strings.Contains(output, "index.ts:2:3") {
		t.Errorf("the root's own file was not judged, so the run proves nothing about the other:\n%s", output)
	}
}
