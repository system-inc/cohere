package parity

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// requireFixedFiles fails the test unless, for each file of a differences fixture's folder that can hold records (see
// recordFiles) and has a fixed file beside it, named for it with .fixed added, as `differences.go.fixed`, every fix the
// port offers in that file, on any of portFindings, a finding both make or one it alone makes, applied, makes the
// fixed file byte for byte. It pins what a `fixes:` record cannot, each edit's exact span and the source it leaves,
// so a move's corrected fix is held whole as much as a lone one.
//
// Nothing is formatted first, so an edit's stray byte counts, as one that eats the space before a comment, which gofmt
// would put back, or a carriage return a raw literal keeps, which gofmt drops. That is why it is not a `.golden` file:
// a `.golden` file beside a fixture is a formatted output, which RequireStaticcheckGolden and
// RequireUpstreamWantsWithFixes hold by formatting the fixed source first, and the tree already holds that convention,
// while a `.fixed` file is the source as the port's edits leave it, unformatted.
//
// In a file with a fixed file, a move must also prove the original's fix wrong: for each of moved, the pairs the
// fixture's `moves:` records make, the original's fixes on its finding, applied in place of the port's on its own,
// must not make the fixed file, since a move whose original fix were right would be no difference in what either
// leaves. A move of span alone, whose two findings offer the same fix, so fails there, and belongs in a file with no
// fixed file.
//
// An edit offered twice, as a package and its test variant each offer it, is applied once, as RunWithSuggestedFixes's
// merge coalesces identical edits; two fixes whose edits overlap, as alternatives on one finding do, fail, so a file
// whose findings offer alternatives takes no fixed file. A failure names the first line the two part on. A file with
// no fixed file is held by its records alone, and a fixed file with no such source beside it fails (see
// requireFixedSources). fileName names a file as findings name theirs, and fixture names the
// fixture in a failure.
func requireFixedFiles(
	test testing.TB,
	folder string,
	fileName func(string) string,
	fixture string,
	portFindings []Finding,
	moved []movedPair,
) {
	test.Helper()
	requireFixedSources(test, folder, fileName)
	for _, pattern := range recordFiles {
		names, globError := filepath.Glob(filepath.Join(folder, pattern))
		if globError != nil {
			test.Fatalf("listing the differences fixture %s: %v", folder, globError)
		}
		for _, name := range names {
			file := fileName(name)
			expected, readError := os.ReadFile(name + ".fixed")
			if errors.Is(readError, fs.ErrNotExist) {
				continue
			}
			if readError != nil {
				test.Fatalf("reading the fixed file of %s: %v", file, readError)
			}
			source, sourceError := os.ReadFile(name)
			if sourceError != nil {
				test.Fatalf("reading the differences fixture: %v", sourceError)
			}
			edits := editsIn(file, portFindings)
			fixedFile := goldenFile{name: file + ".fixed", packageName: fixture, source: name}
			fixed, isFixed := appliedExactly(test, fixedFile, source, edits)
			if isFixed && !bytes.Equal(fixed, expected) {
				test.Errorf("the port's fixes in %s, applied, do not make %s: %s", file, fixedFile.name,
					firstDifference(fixed, expected))
			}
			for _, pair := range moved {
				if pair.original.File != file {
					continue
				}
				// The original's edits stand in for the port's on the pair, and every other edit of the port's stays.
				portsOwn := editsIn(file, []Finding{pair.port})
				substituted := slices.DeleteFunc(slices.Clone(edits), func(edit Edit) bool {
					return slices.Contains(portsOwn, edit)
				})
				for _, edit := range editsIn(file, []Finding{pair.original}) {
					if !slices.Contains(substituted, edit) {
						substituted = append(substituted, edit)
					}
				}
				// Edits that overlap make no file, so they cannot make the fixed one.
				originalFixed, _, isApplied := editsApplied(source, substituted)
				if isApplied && bytes.Equal(originalFixed, expected) {
					test.Errorf("the original's fixes on its moved finding at %s:%d, applied in place of the port's, "+
						"make %s too, so the move proves no fix of the original's wrong", file, pair.original.Line,
						fixedFile.name)
				}
			}
		}
	}
}

// requireFixedSources fails the test for each `.fixed` file in folder whose source, its name without .fixed, is no
// file there that can hold records (see recordFiles), named by fileName, since requireFixedFiles reads a fixed file
// only beside such a file, and one whose source was renamed or never was would be held by nothing.
func requireFixedSources(test testing.TB, folder string, fileName func(string) string) {
	test.Helper()
	fixedFiles, globError := filepath.Glob(filepath.Join(folder, "*.fixed"))
	if globError != nil {
		test.Fatalf("listing the fixed files of the differences fixture %s: %v", folder, globError)
	}
	for _, fixed := range fixedFiles {
		source := strings.TrimSuffix(fixed, ".fixed")
		isRecordFile := slices.ContainsFunc(recordFiles, func(pattern string) bool {
			// Ignored: the patterns are fixed and well formed, so matching never errs.
			matches, _ := filepath.Match(pattern, filepath.Base(source))
			return matches
		})
		_, statError := os.Stat(source)
		if !isRecordFile || statError != nil {
			test.Errorf("%s is a fixed file with no source beside it that holds records, %s, so nothing reads it: "+
				"rename it for its source, or delete it", fileName(fixed), path.Base(fileName(source)))
		}
	}
}

// editsIn is every edit of the fixes of findings that lands in file, as findings name it, each once.
func editsIn(file string, findings []Finding) []Edit {
	edits := []Edit{}
	for _, finding := range findings {
		for _, fix := range finding.Fixes {
			for _, edit := range fix.Edits {
				if edit.File == file && !slices.Contains(edits, edit) {
					edits = append(edits, edit)
				}
			}
		}
	}
	return edits
}

// firstDifference is where fixed and expected, which differ, first part: the line, counted from 1, and that line of
// each with its newline, quoted so a stray space or carriage return shows, and empty past either's end.
func firstDifference(fixed []byte, expected []byte) string {
	fixedLines := strings.SplitAfter(string(fixed), "\n")
	expectedLines := strings.SplitAfter(string(expected), "\n")
	lineAt := func(lines []string, index int) string {
		if index < len(lines) {
			return lines[index]
		}
		return ""
	}
	for index := range max(len(fixedLines), len(expectedLines)) {
		if lineAt(fixedLines, index) != lineAt(expectedLines, index) {
			return fmt.Sprintf("on line %d they leave %q, and it holds %q", index+1, lineAt(fixedLines, index),
				lineAt(expectedLines, index))
		}
	}
	// Two texts split after the same newlines into the same lines are the same text.
	return "they leave the same lines"
}
