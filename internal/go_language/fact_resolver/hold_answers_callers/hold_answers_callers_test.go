package hold_answers_callers_test

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Only a test puts a hand on a run's answers: no file of the module but a test's calls HoldAnswers, so no cohere-go run
// ever answers from anything but its own packages.
func TestOnlyATestHoldsTheAnswers(test *testing.T) {
	test.Parallel()
	// The module root, four directories up from internal/go_language/fact_resolver/hold_answers_callers.
	root, rootError := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if rootError != nil {
		test.Fatal(rootError)
	}
	var callers []string
	walkError := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkError error) error {
		if walkError != nil {
			return walkError
		}
		if entry.IsDir() {
			if name := entry.Name(); path != root && (strings.HasPrefix(name, ".") || name == "testdata" ||
				name == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		contents, readError := os.ReadFile(path)
		if readError != nil {
			return readError
		}
		for _, line := range bytes.Split(contents, []byte("\n")) {
			if bytes.Contains(line, []byte("HoldAnswers(")) &&
				!bytes.HasPrefix(line, []byte("func (resolver *Resolver) HoldAnswers(")) &&
				!bytes.HasPrefix(bytes.TrimSpace(line), []byte("//")) {
				callers = append(callers, path)
			}
		}
		return nil
	})
	if walkError != nil {
		test.Fatal(walkError)
	}
	if len(callers) > 0 {
		test.Errorf("want HoldAnswers called only from tests, got calls in %q", callers)
	}
}
