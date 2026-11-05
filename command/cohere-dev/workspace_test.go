//go:build unix

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The gate refuses a go.mod go.work doesn't use, by path and with the use line to add, rather than leave it
// ungated, which is how the shims went untested. Reading the workspace itself refuses nothing, since the Go
// engine takes such a module for a project of its own (internal/goengine/project), so the refusal is the
// gate's.
func TestReadGatedWorkspaceRefusesAModuleGoWorkDoesNotUse(test *testing.T) {
	test.Parallel()
	repository := test.TempDir()
	if output, initError := exec.Command("git", "init", "-q", repository).CombinedOutput(); initError != nil {
		test.Fatalf("making the repository: %v\n%s", initError, output)
	}
	files := map[string]string{
		"go.work":         "go 1.21\n\nuse .\n",
		"go.mod":          "module example.com/root\n\ngo 1.21\n",
		"unlisted/go.mod": "module example.com/root/unlisted\n\ngo 1.21\n",
	}
	for name, contents := range files {
		path := filepath.Join(repository, name)
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			test.Fatal(makeError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}

	_, refusal := readGatedWorkspace(repository)
	if refusal == nil || !strings.Contains(refusal.Error(), "unlisted/go.mod: add `./unlisted` to go.work's use") {
		test.Fatalf("an unlisted module was not refused by path: %v", refusal)
	}

	listed := []byte("go 1.21\n\nuse (\n\t.\n\t./unlisted\n)\n")
	if writeError := os.WriteFile(filepath.Join(repository, "go.work"), listed, 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	if _, readError := readGatedWorkspace(repository); readError != nil {
		test.Errorf("with the module used, reading the workspace still failed: %v", readError)
	}
}
