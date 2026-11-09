package parity

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
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
// An edit offered twice, as a package and its test variant each offer it, is applied once, as RunWithSuggestedFixes's
// merge coalesces identical edits; two fixes whose edits overlap, as alternatives on one finding do, fail, so a file
// whose findings offer alternatives takes no fixed file. A failure names the first line the two part on. A file with
// no fixed file is held by its records alone. fileName names a file as findings name theirs, and fixture names the
// fixture in a failure.
func requireFixedFiles(
	test testing.TB,
	folder string,
	fileName func(string) string,
	fixture string,
	portFindings []Finding,
) {
	test.Helper()
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
			edits := []Edit{}
			for _, finding := range portFindings {
				for _, fix := range finding.Fixes {
					for _, edit := range fix.Edits {
						if edit.File == file && !slices.Contains(edits, edit) {
							edits = append(edits, edit)
						}
					}
				}
			}
			fixedFile := goldenFile{name: file + ".fixed", packageName: fixture, source: name}
			fixed, isFixed := appliedExactly(test, fixedFile, source, edits)
			if isFixed && !bytes.Equal(fixed, expected) {
				test.Errorf("the port's fixes in %s, applied, do not make %s: %s", file, fixedFile.name,
					firstDifference(fixed, expected))
			}
		}
	}
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
