package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestOneFixFormatRunSettlesAPrintedArrow is the case that found the gap, through the real rule and the
// real printer: a one-line block-bodied arrow that the printer breaks across lines, under
// nexus/consistency-no-multiline-arrow-function, which repairs a multi-line arrow. Found as 51 arrows
// in 23 of www-phi-health's files, which one `--fix --format` formatted and left for the next run's
// fixer, so the run's own `--no-fix` check flagged them.
//
// One writing run must leave nothing for the check: the fix the printed text triggers lands in the
// same run, and the fixed text is formatted again.
func TestOneFixFormatRunSettlesAPrintedArrow(t *testing.T) {
	binary := buildCohere(t)
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"tsconfig.json":            fixScopeTsconfig,
		"CohereSettings.json":      `{"extends":"./NexusCohereSettings.json","rules":{"nexus/consistency-no-multiline-arrow-function":"error"}}`,
		"NexusCohereSettings.json": `{"format":{"tabWidth":4,"singleQuote":true,"printWidth":120,"ignore":[]}}`,
		"Probe.ts":                 "export function register(listener: () => void): void {\n    listener();\n}\n\nregister(() => { console.log('ready'); });\n",
	})

	output, code := runCohere(t, binary, root, "--fix", "--format", "Probe.ts")
	if code != 0 {
		t.Fatalf("the writing run exited %d:\n%s", code, output)
	}
	written := readForTest(t, filepath.Join(root, "Probe.ts"))
	if !strings.Contains(written, "register(function() {\n    console.log('ready');\n});") {
		t.Fatalf("the arrow the printer broke across lines was not repaired in the same run:\n%s\noutput:\n%s", written, output)
	}

	output, code = runCohere(t, binary, root, "--no-fix", "--format", "Probe.ts")
	if code != 0 || !strings.Contains(output, "0 files would change") {
		t.Fatalf("one --fix --format run left work for the next, exit %d:\n%s", code, output)
	}
}
