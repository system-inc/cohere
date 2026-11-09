package guard

import (
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/build_inputs"
)

// The cohere binary carries no part of staticcheck's intermediate representation (#7cv09fg): every rule reads the
// engine's own, and TestNoCodeImportsStaticchecksIntermediateRepresentation holds that no file of ours imports go/ir,
// but a file of ours can still reach it through something it imports, as the registry did through staticcheck's
// suites, so this reads the whole of what the binary links, from go list. The same listing over staticcheck's own
// suite names every one of the paths, so a listing that read nothing, or a path the list spells otherwise, fails here
// rather than reading clean.
func TestTheBinaryLinksNoStaticcheckIntermediateRepresentation(test *testing.T) {
	test.Parallel()
	linked := linkedPackages(test, "./command/cohere")
	if len(linked) < 100 {
		test.Fatalf("go list names %d packages the binary links, too few to be the binary's", len(linked))
	}
	for _, importPath := range staticcheckIntermediateRepresentation {
		if slices.Contains(linked, importPath) {
			test.Errorf("the cohere binary links %s, staticcheck's intermediate representation, through a package "+
				"it imports: find the importer with `go list -deps -f '{{.ImportPath}} {{.Imports}}' ./command/cohere`",
				importPath)
		}
	}
	suite := linkedPackages(test, "honnef.co/go/tools/staticcheck")
	for _, importPath := range staticcheckIntermediateRepresentation {
		if !slices.Contains(suite, importPath) {
			test.Errorf("staticcheck's own suite links no %s by go list, so the binary's listing could not have "+
				"found it either", importPath)
		}
	}
}

// linkedPackages is every package a build of pattern links, its own included, by import path, from one go list run
// from the module root, its inputs recorded so Go's test cache keys the result on every file the build reads.
func linkedPackages(test *testing.T, pattern string) []string {
	test.Helper()
	// Tests run in their own package directory, and the pattern is written relative to the module root.
	root := filepath.Join("..", "..")
	build_inputs.Record(test, root, pattern)
	// A worktree's VCS state can't be read in every checkout, and listing what links needs none of it.
	command := exec.Command("go", "list", "-buildvcs=false", "-deps", pattern)
	command.Dir = root
	var stderr strings.Builder
	command.Stderr = &stderr
	output, outputError := command.Output()
	if outputError != nil {
		test.Fatalf("go list -deps %s: %v\n%s", pattern, outputError, stderr.String())
	}
	return strings.Fields(string(output))
}
