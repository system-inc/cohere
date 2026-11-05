package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestPipelineRefusesAZeroMatchOverrideSelector proves the validation is in the live command, not
// merely available as an unused configuration helper.
//
// The second run is the mutation control: changing only the selector from an impossible path to the
// file that exists must turn the configuration failure into a successful lint run. Without the call
// in run(), the first invocation exits cleanly and this test catches the original vacuous pass.
func TestPipelineRefusesAZeroMatchOverrideSelector(test *testing.T) {
	test.Parallel()

	binary := buildCohere(test)

	directory := test.TempDir()
	writeSelectorValidationProject(test, directory, "missing/**")

	command := exec.Command(binary, "--lint", "--no-fix", "--directory", directory)
	output, outputError := command.CombinedOutput()
	if outputError == nil {
		test.Fatalf("the pipeline exited successfully with a zero-match override selector:\n%s", output)
	}
	if !strings.Contains(string(output), "missing/**") {
		test.Fatalf("the pipeline failure does not name the selector to repair:\n%s", output)
	}

	writeSelectorValidationProject(test, directory, "**/*.ts")
	command = exec.Command(binary, "--lint", "--no-fix", "--directory", directory)
	if output, outputError := command.CombinedOutput(); outputError != nil {
		test.Fatalf("the same project failed after its selector was changed to match index.ts: %v\n%s", outputError, output)
	}
}

func writeSelectorValidationProject(test *testing.T, directory string, selector string) {
	test.Helper()

	files := map[string]string{
		"tsconfig.json": `{"compilerOptions":{"strict":true},"include":["*.ts"]}`,
		"index.ts":      "export const value = 1;\n",
		"CohereSettings.json": `{"rules":{},"overrides":[{"files":["` + selector +
			`"],"rules":{"no-debugger":"off"}}]}`,
	}
	for name, contents := range files {
		if writeError := os.WriteFile(filepath.Join(directory, name), []byte(contents), 0o644); writeError != nil {
			test.Fatalf("writing %s: %v", name, writeError)
		}
	}
}
