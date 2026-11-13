package main

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// A corpus is checked out layer by layer as repositories of their own: the outer one at its pin, and the nested one
// cloned into the directory its gitlink names, at the pin the gitlink reads, so the outer checkout sees an initialized
// submodule at its recorded commit and both statuses are empty. Nothing in it is a symbolic link.
func TestPrepareCorpusChecksOutEachLayerAsARepository(test *testing.T) {
	test.Parallel()
	sources := test.TempDir()
	inner := filepath.Join(sources, "inner")
	innerCommit := makeRepository(test, inner, map[string]string{"tsc/go.mod": "module tsc\n"}, nil)
	outer := filepath.Join(sources, "outer")
	outerCommit := makeRepository(test, outer, map[string]string{"go.work": "go 1.27\n\nuse ./TypeScript/tsc\n"},
		map[string]string{"TypeScript": innerCommit})
	pinned, gitlinkError := gitlinkOf(outer, outerCommit, "TypeScript")
	if gitlinkError != nil || pinned != innerCommit {
		test.Fatalf("the fixture's gitlink reads %s, %v; want %s", pinned, gitlinkError, innerCommit)
	}

	output := test.TempDir()
	if prepareError := prepareCorpus(output, corpus{name: "nested", commit: outerCommit, layers: []corpusLayer{
		{repository: outer, commit: outerCommit},
		{repository: inner, commit: pinned, within: "TypeScript"},
	}}, progress{writer: io.Discard}); prepareError != nil {
		test.Fatal(prepareError)
	}

	root := corpusDirectory(output, "nested")
	for _, checkout := range []struct {
		directory string
		commit    string
	}{{root, outerCommit}, {filepath.Join(root, "TypeScript"), innerCommit}} {
		if information, statError := os.Stat(filepath.Join(checkout.directory, ".git")); statError != nil ||
			!information.IsDir() {
			test.Errorf("%s is not a repository of its own: %v", checkout.directory, statError)
		}
		if head := runGit(test, checkout.directory, "rev-parse", "HEAD"); head != checkout.commit {
			test.Errorf("%s is at %s, want %s", checkout.directory, head, checkout.commit)
		}
		if status := runGit(test, checkout.directory, "status", "--porcelain"); status != "" {
			test.Errorf("%s's status is not empty:\n%s", checkout.directory, status)
		}
	}
	if _, readError := os.ReadFile(filepath.Join(root, "TypeScript", "tsc", "go.mod")); readError != nil {
		test.Errorf("the nested layer's file is not in the corpus: %v", readError)
	}
	walkError := filepath.WalkDir(root, func(path string, entry fs.DirEntry, entryError error) error {
		if entryError == nil && entry.Type()&fs.ModeSymlink != 0 {
			test.Errorf("%s is a symbolic link", path)
		}
		return entryError
	})
	if walkError != nil {
		test.Fatal(walkError)
	}
}
