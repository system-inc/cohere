package dispatch

import (
	"os"
	"path/filepath"
	"testing"
)

// A hash that misses an input is the defect this whole package exists to prevent: it serves a
// binary built from old rules while the file on disk says the rule is enforced, and the gate prints
// green. Every test here asserts that some specific change to the inputs moves the digest.

func TestHashChangesWhenFileContentsChange(test *testing.T) {
	test.Parallel()

	// The case a filename-based hash misses entirely: a rule edited in place keeps its name.
	directory := test.TempDir()
	path := filepath.Join(directory, "rule.go")
	writeFile(test, path, "package rules\n")

	inputs := Inputs{
		GoVersion:          "go1.27.0 darwin arm64",
		TypeScriptGoCommit: "2b82831a05b6b99da279d12fecbcbc460574a85b",
		BuildFlags:         ReleaseBuildFlags,
		SourceFiles:        []string{path},
	}

	before := compute(test, inputs)

	writeFile(test, path, "package rules\n\n// an edit\n")
	after := compute(test, inputs)

	if before == after {
		test.Fatalf("editing a file left the hash at %s, so a rule change would serve a stale binary", before)
	}
}

func TestHashChangesWhenContentsChangeWithoutChangingLength(test *testing.T) {
	test.Parallel()

	// This is the test that proves the digest reads the bytes rather than a summary of them.
	//
	// It exists because the obvious version of the previous test does not. Each file is framed in
	// the digest with its name and its length, so an edit that changes the length moves the hash
	// through the length field alone, and a Compute that never hashed a single byte of content
	// still passed. A same-length edit removes every signal but the contents themselves.
	directory := test.TempDir()
	path := filepath.Join(directory, "rule.go")
	writeFile(test, path, "package rules // aaa\n")

	inputs := Inputs{SourceFiles: []string{path}}
	before := compute(test, inputs)

	writeFile(test, path, "package rules // bbb\n")
	after := compute(test, inputs)

	if before == after {
		test.Fatalf("a same-length edit left the hash at %s, so the digest is not reading file contents", before)
	}
}

func TestHashChangesWithEachInput(test *testing.T) {
	test.Parallel()

	// Each field is a thing that changes what the binary does. Missing any one of them is a
	// separate way for the cache to lie, so each gets its own assertion rather than one combined
	// check that could pass on the strength of the others.
	directory := test.TempDir()
	path := filepath.Join(directory, "rule.go")
	writeFile(test, path, "package rules\n")

	base := Inputs{
		GoVersion:          "go1.27.0 darwin arm64",
		TypeScriptGoCommit: "2b82831a05b6b99da279d12fecbcbc460574a85b",
		BuildFlags:         ReleaseBuildFlags,
		SourceFiles:        []string{path},
	}
	baseHash := compute(test, base)

	tests := []struct {
		name   string
		modify func(inputs *Inputs)
	}{
		{"a different Go toolchain", func(inputs *Inputs) { inputs.GoVersion = "go1.26.0 darwin arm64" }},
		{"a different platform", func(inputs *Inputs) { inputs.GoVersion = "go1.27.0 linux amd64" }},
		{
			"a different typescript-go commit",
			func(inputs *Inputs) { inputs.TypeScriptGoCommit = "0000000000000000000000000000000000000000" },
		},
		{"different build flags", func(inputs *Inputs) { inputs.BuildFlags = []string{"-trimpath"} }},
		// The binary is stamped with both and reports them, so a key without them serves a binary built in
		// one state to a tree in another (#50wtb2s).
		{"a different commit", func(inputs *Inputs) { inputs.SelfCommit = "508863131ed19b76776da9cd08862e4ef39d00b0" }},
		{"a modified tree", func(inputs *Inputs) { inputs.SourceTreeModified = true }},
	}

	for _, testCase := range tests {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()

			changed := base
			testCase.modify(&changed)

			if got := compute(test, changed); got == baseHash {
				test.Fatalf(
					"%s did not change the hash, so the cache would serve a binary built from the other one",
					testCase.name,
				)
			}
		})
	}
}

func TestHashIsStableAcrossRuns(test *testing.T) {
	test.Parallel()

	// An unstable hash misses the cache forever, rebuilding on every invocation. That is the
	// opposite failure from staleness and just as much a defect.
	directory := test.TempDir()
	path := filepath.Join(directory, "rule.go")
	writeFile(test, path, "package rules\n")

	inputs := Inputs{
		GoVersion:          "go1.27.0 darwin arm64",
		TypeScriptGoCommit: "2b82831a05b6b99da279d12fecbcbc460574a85b",
		BuildFlags:         ReleaseBuildFlags,
		SourceFiles:        []string{path},
	}

	first := compute(test, inputs)
	for attempt := range 5 {
		if got := compute(test, inputs); got != first {
			test.Fatalf("run %d hashed to %s where the first run hashed to %s", attempt+2, got, first)
		}
	}
}

func TestHashIgnoresSourceFileOrder(test *testing.T) {
	test.Parallel()

	// The toolchain is free to list packages in whatever order it likes, and an order-sensitive
	// hash would miss the cache for reasons that have nothing to do with the code.
	directory := test.TempDir()
	first := filepath.Join(directory, "a.go")
	second := filepath.Join(directory, "b.go")
	writeFile(test, first, "package rules\n")
	writeFile(test, second, "package rules\n\n// b\n")

	forward := compute(test, Inputs{SourceFiles: []string{first, second}})
	backward := compute(test, Inputs{SourceFiles: []string{second, first}})

	if forward != backward {
		test.Fatalf("listing the same files in a different order hashed to %s and %s", forward, backward)
	}
}

func TestHashDistinguishesFilesWithSharedContent(test *testing.T) {
	test.Parallel()

	// Two files whose contents are identical must not be interchangeable with one file: the digest
	// frames each entry with its name and length so that concatenation cannot collide.
	directory := test.TempDir()
	first := filepath.Join(directory, "a.go")
	second := filepath.Join(directory, "b.go")
	writeFile(test, first, "package rules\n")
	writeFile(test, second, "package rules\n")

	one := compute(test, Inputs{SourceFiles: []string{first}})
	both := compute(test, Inputs{SourceFiles: []string{first, second}})

	if one == both {
		test.Fatal("adding a second file with identical contents did not change the hash")
	}
}

func TestComputeFailsOnUnreadableInput(test *testing.T) {
	test.Parallel()

	// Hashing around a file that was listed but cannot be read produces a stable digest for a tree
	// we could not actually see, which is staleness wearing a different hat.
	inputs := Inputs{SourceFiles: []string{filepath.Join(test.TempDir(), "absent.go")}}

	if _, computeError := inputs.Compute(); computeError == nil {
		test.Fatal("hashing a missing input succeeded, so an unreadable tree would hash as if it were fine")
	}
}

func TestCollectInputsRejectsAnEmptyFileSet(test *testing.T) {
	test.Parallel()

	// An empty input set hashes to a perfectly stable digest for a tree containing nothing, and
	// every later run hits that entry. A run that measured nothing must not look like a run that
	// found nothing.
	_, collectInputsError := CollectInputs(test.TempDir(), "./cmd/does-not-exist", ReleaseBuildFlags)
	if collectInputsError == nil {
		test.Fatal("collecting inputs for a package that does not exist succeeded")
	}
}

// compute hashes inputs, failing the test rather than returning an error.
func compute(test *testing.T, inputs Inputs) string {
	test.Helper()

	hash, computeError := inputs.Compute()
	if computeError != nil {
		test.Fatalf("computing the hash: %v", computeError)
	}
	if hash == "" {
		test.Fatal("the hash came back empty")
	}
	return hash
}

// writeFile writes contents, creating its directory, failing the test on error.
func writeFile(test *testing.T, path string, contents string) {
	test.Helper()

	if createDirectoryError := os.MkdirAll(filepath.Dir(path), 0o755); createDirectoryError != nil {
		test.Fatalf("creating the directory for %s: %v", path, createDirectoryError)
	}
	if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
		test.Fatalf("writing %s: %v", path, writeError)
	}
}
