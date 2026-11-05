package release

import (
	"path/filepath"
	"strings"
	"testing"
)

// The Swift engine's build directory is never inside the output, which the release packs and uploads
// whole, and a directory that only shares the output's name as a prefix is not inside it.
func TestTheSwiftScratchIsNeverInsideTheOutput(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	output := filepath.Join(root, "dist")

	scratch, err := swiftScratchDirectory(Options{OutputDirectory: output})
	if err != nil {
		test.Fatalf("the default was refused: %v", err)
	}
	if strings.HasPrefix(scratch, output+string(filepath.Separator)) {
		test.Fatalf("the default %s is inside the output %s", scratch, output)
	}

	for _, testCase := range []struct {
		scratch string
		allowed bool
	}{
		{filepath.Join(output, ".swift-build"), false},
		{filepath.Join(output, "a", "b"), false},
		{output, false},
		{filepath.Join(root, "dist-scratch"), true},
		{filepath.Join(root, "elsewhere"), true},
		{root, true},
	} {
		_, err := swiftScratchDirectory(Options{OutputDirectory: output, SwiftScratchDirectory: testCase.scratch})
		if (err == nil) != testCase.allowed {
			test.Errorf("%s: allowed %v, want %v (%v)", testCase.scratch, err == nil, testCase.allowed, err)
		}
	}
}

// Build holds the rule, before anything is compiled: a scratch inside the output is refused by name.
func TestBuildRefusesASwiftScratchInsideTheOutput(test *testing.T) {
	test.Parallel()

	output := test.TempDir()
	_, err := Build(Options{
		Version:               "1.0.0",
		ModuleDirectory:       test.TempDir(),
		OutputDirectory:       output,
		SwiftScratchDirectory: filepath.Join(output, ".swift-build"),
	})
	if err == nil || !strings.Contains(err.Error(), "is inside the output") {
		test.Fatalf("Build accepted a Swift scratch inside its output: %v", err)
	}
}
