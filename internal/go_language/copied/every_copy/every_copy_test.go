package every_copy

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_language/copied/upstream_text"
)

// skippedDirectories are what the walk leaves out: the TypeScript submodule, which is upstream's own tree rather
// than a copy in ours; npm's node_modules; and testdata, whose planted copies upstream_text's tests read and whose
// recorded upstream text declares no copy.
var skippedDirectories = []string{"TypeScript", "node_modules", "testdata"}

// recordedDirectory is upstream_text's recorded tree, from this package's directory, and recordedSpelled the same
// from the module root, as a message names it.
var recordedDirectory = filepath.Join("..", "upstream_text", filepath.FromSlash(upstream_text.RecordedDirectory))

const recordedSpelled = "internal/go_language/copied/upstream_text/" + upstream_text.RecordedDirectory

// TestEveryCopyMatchesUpstreamOutsideItsMarkedChanges walks the module for every Go file outside testdata that
// declares itself a copy and holds each to its recorded upstream file. Every recorded file is one a copy names, or
// its source's LICENSE, so text left behind by a copy bumped or deleted fails rather than lingering.
func TestEveryCopyMatchesUpstreamOutsideItsMarkedChanges(test *testing.T) {
	test.Parallel()

	upstream, recordedError := upstream_text.ReadRecorded(os.DirFS(recordedDirectory))
	if recordedError != nil {
		test.Fatal(recordedError)
	}
	// This package sits four directories below the module root.
	root, rootError := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if rootError != nil {
		test.Fatalf("resolving the module root: %v", rootError)
	}
	var copies []string
	named := map[string]bool{}
	walkError := filepath.WalkDir(root, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil {
			return pathError
		}
		if entry.IsDir() {
			if path != root && (strings.HasPrefix(entry.Name(), ".") || slices.Contains(skippedDirectories,
				entry.Name())) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(entry.Name()) != ".go" {
			return nil
		}
		contents, readError := os.ReadFile(path)
		if readError != nil {
			return readError
		}
		relative, relativeError := filepath.Rel(root, path)
		if relativeError != nil {
			return relativeError
		}
		header, declared, problems := upstream_text.Check(upstream, filepath.ToSlash(relative), contents)
		if declared {
			copies = append(copies, filepath.ToSlash(relative))
			named[upstream_text.RecordedPath(header)] = true
		}
		for _, problem := range problems {
			test.Error(problem)
		}
		return nil
	})
	if walkError != nil {
		test.Fatalf("walking %s: %v", root, walkError)
	}
	// A walk that found no copy would pass over a tree nobody examined, so the copies cohere-go carries today
	// must be among those found.
	for _, expected := range []string{
		"internal/go_language/check/analysistest/analysistest.go",
		"internal/go_language/check/astutil/free/free.go",
		"internal/go_language/check/checker/checker.go",
		"internal/go_language/check/checker/driverutil/fix.go",
		"internal/go_language/check/checker/driverutil/readfile.go",
		"internal/go_language/check/checker/driverutil/url.go",
		"internal/go_language/check/checker/driverutil/validatefix.go",
		"internal/go_language/check/diff/diff.go",
		"internal/go_language/check/diff/lcs/common.go",
		"internal/go_language/check/diff/lcs/doc.go",
		"internal/go_language/check/diff/lcs/labels.go",
		"internal/go_language/check/diff/lcs/old.go",
		"internal/go_language/check/diff/lcs/sequence.go",
		"internal/go_language/check/diff/merge.go",
		"internal/go_language/check/diff/ndiff.go",
		"internal/go_language/check/diff/unified.go",
		"internal/go_language/check/internal/internal.go",
		"internal/go_language/export_data/aliases/aliases.go",
		"internal/go_language/export_data/bimport.go",
		"internal/go_language/export_data/iexport.go",
		"internal/go_language/export_data/iimport.go",
		"internal/go_language/export_data/predeclared.go",
		"internal/go_language/export_data/typesinternal/recv.go",
		"internal/go_language/export_data/typesinternal/varkind.go",
		"internal/go_language/load/build/build.go",
		"internal/go_language/load/build/read.go",
		"internal/go_language/load/build/syslist/syslist.go",
	} {
		if !slices.Contains(copies, expected) {
			test.Errorf("the walk did not find %s among the copies it found, %q", expected, copies)
		}
	}
	for _, recorded := range upstream.Listed() {
		if !strings.HasSuffix(recorded, "/LICENSE") && !named[recorded] {
			test.Errorf("%s/%s is recorded, but no copy names it: delete it and its line in SHA256SUMS",
				recordedSpelled, recorded)
		}
	}
}
