package copied

import (
	"bytes"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"golang.org/x/mod/module"
)

// A file is a copy only while it is one. This holds every file that declares itself a copy to its upstream file in
// the module cache, or in the toolchain's source for a copy of the standard library: each run of lines where the two
// differ holds a cohere: comment, and every other line is upstream's, byte for byte. It is a test rather than a lint
// rule because it reads upstream's source, which only the module cache and the toolchain hold. A module missing from
// the cache fails, never skips, since a skipped check reads as a copy that matched. A copy of the standard library is
// held to the toolchain's source, whatever release the toolchain is; what keeps the toolchain on the copy's release is
// the copy's own package's test (load/build's TestTheToolchainsGoBuildIsTheCopys), since only it knows which releases
// ship the sources it copied.

// skippedDirectories are what the walk leaves out: the TypeScript submodule, which is upstream's own tree rather
// than a copy in ours; npm's node_modules; and testdata, whose planted copies TestPlantedCopies reads.
var skippedDirectories = []string{"TypeScript", "node_modules", "testdata"}

// TestEveryCopyMatchesUpstreamOutsideItsMarkedChanges walks the module for every Go file outside testdata that
// declares itself a copy and holds each to its upstream file.
func TestEveryCopyMatchesUpstreamOutsideItsMarkedChanges(test *testing.T) {
	test.Parallel()

	// This package sits three directories below the module root.
	root, rootError := filepath.Abs(filepath.Join("..", "..", ".."))
	if rootError != nil {
		test.Fatalf("resolving the module root: %v", rootError)
	}
	var copies []string
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
		declared, problems := checkCopy(filepath.ToSlash(relative), contents)
		if declared {
			copies = append(copies, filepath.ToSlash(relative))
		}
		for _, problem := range problems {
			test.Error(problem)
		}
		return nil
	})
	if walkError != nil {
		test.Fatalf("walking %s: %v", root, walkError)
	}
	// A walk that found no copy would pass over a tree nobody examined, so the copies the engine carries today
	// must be among those found.
	for _, expected := range []string{
		"internal/go_engine/check/analysistest/analysistest.go",
		"internal/go_engine/check/astutil/free/free.go",
		"internal/go_engine/check/checker/checker.go",
		"internal/go_engine/check/checker/driverutil/fix.go",
		"internal/go_engine/check/checker/driverutil/readfile.go",
		"internal/go_engine/check/checker/driverutil/url.go",
		"internal/go_engine/check/checker/driverutil/validatefix.go",
		"internal/go_engine/check/diff/diff.go",
		"internal/go_engine/check/diff/lcs/common.go",
		"internal/go_engine/check/diff/lcs/doc.go",
		"internal/go_engine/check/diff/lcs/labels.go",
		"internal/go_engine/check/diff/lcs/old.go",
		"internal/go_engine/check/diff/lcs/sequence.go",
		"internal/go_engine/check/diff/merge.go",
		"internal/go_engine/check/diff/ndiff.go",
		"internal/go_engine/check/diff/unified.go",
		"internal/go_engine/check/internal/internal.go",
		"internal/go_engine/load/build/build.go",
		"internal/go_engine/load/build/read.go",
		"internal/go_engine/load/build/syslist/syslist.go",
	} {
		if !slices.Contains(copies, expected) {
			test.Errorf("the walk did not find %s among the copies it found, %q", expected, copies)
		}
	}
}

// TestPlantedCopies holds the check to both answers on planted copies of x/tools' driverutil/url.go: a faithful
// copy with a marked change passes, and an unmarked edit, a header with no until and a module the cache does not
// hold each fail, naming what failed.
func TestPlantedCopies(test *testing.T) {
	test.Parallel()
	for _, planted := range []struct {
		file string

		// saying are what the problems say, one each, in order; none for a copy that passes.
		saying []string
	}{
		{file: "faithful/url.go"},
		{file: "unmarked/url.go", saying: []string{"unmarked/url.go:24: differs from golang.org/x/tools@v0.50.0 " +
			"internal/analysis/driverutil/url.go:20 with no cohere: comment among the lines that differ"}},
		{file: "no-until/url.go", saying: []string{"no-until/url.go: reading the copy header: no cohere:until line " +
			"right below the cohere:copied line"}},
		{file: "uncached/url.go", saying: []string{"uncached/url.go: finding example.com/uncached@v1.0.0 in the " +
			"module cache"}},
	} {
		test.Run(planted.file, func(test *testing.T) {
			test.Parallel()
			contents, readError := os.ReadFile(filepath.Join("testdata", filepath.FromSlash(planted.file)))
			if readError != nil {
				test.Fatalf("reading the planted copy: %v", readError)
			}
			declared, problems := checkCopy(planted.file, contents)
			if !declared {
				test.Fatalf("%s declares no copy, so it plants nothing", planted.file)
			}
			if len(problems) != len(planted.saying) {
				test.Fatalf("got %d problems, want %d:\n%s", len(problems), len(planted.saying),
					strings.Join(problems, "\n"))
			}
			for index, problem := range problems {
				if !strings.HasPrefix(problem, planted.saying[index]) {
					test.Errorf("problem %d says\n%s\nwant it to open with\n%s", index, problem, planted.saying[index])
				}
			}
		})
	}
}

// TestUnmarkedChangesFindsEachRunOnce diffs small line lists, so the hunks the copies rely on are pinned apart
// from any file: an insertion, a replacement and a drop each count as one run, marked by a cohere: comment
// anywhere in it, and the lines around a marked run are not swept into it.
func TestUnmarkedChangesFindsEachRunOnce(test *testing.T) {
	test.Parallel()
	upstream := []string{"a", "}", "", "b", "c", "}", "d"}
	for _, diffed := range []struct {
		name     string
		ours     []string
		expected []unmarkedChange
	}{
		{name: "identical", ours: upstream},
		{name: "a marked insertion with braces in it", ours: []string{"a", "}", "", "// cohere: x", "if x {", "}", "",
			"b", "c", "}", "d"}},
		{name: "an unmarked insertion", ours: []string{"a", "}", "", "x", "b", "c", "}", "d"},
			expected: []unmarkedChange{{oursLine: 4, upstreamLine: 4}}},
		{name: "a marked replacement", ours: []string{"a", "}", "", "\t// cohere: why", "B", "c", "}", "d"}},
		{name: "an unmarked replacement", ours: []string{"a", "}", "", "B", "c", "}", "d"},
			expected: []unmarkedChange{{oursLine: 4, upstreamLine: 4}}},
		{name: "a marked drop", ours: []string{"a", "}", "", "// cohere: b and c are dropped", "}", "d"}},
		{name: "an unmarked drop", ours: []string{"a", "}", "", "}", "d"},
			expected: []unmarkedChange{{oursLine: 4, upstreamLine: 4}}},
		{name: "an unmarked edit beside a marked one", ours: []string{"a", "// cohere: x", "}", "", "B", "c", "}", "d"},
			expected: []unmarkedChange{{oursLine: 5, upstreamLine: 4}}},
		{name: "a marker in code is no comment", ours: []string{"a", "}", "", `x := "cohere: no"`, "b", "c", "}", "d"},
			expected: []unmarkedChange{{oursLine: 4, upstreamLine: 4}}},
		{name: "an unmarked edit at the end", ours: []string{"a", "}", "", "b", "c", "}", "D"},
			expected: []unmarkedChange{{oursLine: 7, upstreamLine: 7}}},
	} {
		test.Run(diffed.name, func(test *testing.T) {
			test.Parallel()
			changes := unmarkedChanges(diffed.ours, upstream)
			if !slices.Equal(changes, diffed.expected) {
				test.Errorf("got %+v, want %+v", changes, diffed.expected)
			}
		})
	}
}

// checkCopy reads the header of the file at path, its contents given, and holds a declared copy to upstream. It
// returns whether the file declares a copy and every problem as a sentence naming the file and, for a line that
// differs, the line and upstream's path.
func checkCopy(path string, contents []byte) (bool, []string) {
	if !bytes.Contains(contents, []byte(copiedDirective)) && !bytes.Contains(contents, []byte(untilDirective)) {
		return false, nil
	}
	file, parseError := parser.ParseFile(token.NewFileSet(), path, contents,
		parser.PackageClauseOnly|parser.ParseComments)
	if parseError != nil {
		return true, []string{fmt.Sprintf("%s: parsing it for a copy header: %v", path, parseError)}
	}
	header, declared, readError := Read(file)
	if !declared {
		return false, nil
	}
	if readError != nil {
		return true, []string{fmt.Sprintf("%s: %v", path, readError)}
	}
	upstreamPath, locateError := upstreamFile(header)
	if locateError != nil {
		return true, []string{fmt.Sprintf("%s: %v", path, locateError)}
	}
	upstream, readUpstreamError := os.ReadFile(upstreamPath)
	if readUpstreamError != nil {
		return true, []string{fmt.Sprintf("%s: reading upstream's %s at %s@%s: %v", path, header.Path,
			header.Module, header.Version, readUpstreamError)}
	}
	var problems []string
	ourLines, upstreamLines := strings.Split(string(contents), "\n"), strings.Split(string(upstream), "\n")
	for _, change := range unmarkedChanges(ourLines, upstreamLines) {
		problems = append(problems, fmt.Sprintf("%s:%d: differs from %s@%s %s:%d with no cohere: comment among "+
			"the lines that differ", path, change.oursLine, header.Module, header.Version, header.Path,
			change.upstreamLine))
	}
	return true, problems
}

// moduleCache is the module cache's directory, asked of go once per test binary.
var moduleCache = sync.OnceValues(func() (string, error) {
	output, goError := exec.Command("go", "env", "GOMODCACHE").Output()
	if goError != nil {
		return "", fmt.Errorf("asking go env for GOMODCACHE: %w", goError)
	}
	directory := strings.TrimSpace(string(output))
	if directory == "" {
		return "", errors.New("asking go env for GOMODCACHE: it printed nothing")
	}
	return directory, nil
})

// toolchainRoot is the toolchain's root, asked of go once per test binary.
var toolchainRoot = sync.OnceValues(func() (string, error) {
	output, goError := exec.Command("go", "env", "GOROOT").Output()
	if goError != nil {
		return "", fmt.Errorf("asking go env for GOROOT: %w", goError)
	}
	root := strings.TrimSpace(string(output))
	if root == "" {
		return "", errors.New("asking go env for GOROOT: it printed nothing")
	}
	return root, nil
})

// upstreamFile is where the module cache holds the file header names, in the cache's escaped form of the module
// path and version, or for a copy of the standard library where the toolchain's source holds it.
func upstreamFile(header Header) (string, error) {
	if header.Module == StandardLibrary {
		root, rootError := toolchainRoot()
		if rootError != nil {
			return "", rootError
		}
		return filepath.Join(root, "src", filepath.FromSlash(header.Path)), nil
	}
	cache, cacheError := moduleCache()
	if cacheError != nil {
		return "", cacheError
	}
	escapedPath, pathError := module.EscapePath(header.Module)
	if pathError != nil {
		return "", fmt.Errorf("escaping %s for the module cache: %w", header.Module, pathError)
	}
	escapedVersion, versionError := module.EscapeVersion(header.Version)
	if versionError != nil {
		return "", fmt.Errorf("escaping %s for the module cache: %w", header.Version, versionError)
	}
	moduleDirectory := filepath.Join(cache, escapedPath+"@"+escapedVersion)
	if _, statError := os.Stat(moduleDirectory); statError != nil {
		return "", fmt.Errorf("finding %s@%s in the module cache at %s: %w; `go mod download %s@%s` puts it there",
			header.Module, header.Version, cache, statError, header.Module, header.Version)
	}
	return filepath.Join(moduleDirectory, filepath.FromSlash(header.Path)), nil
}

// unmarkedChange is a run of lines where a copy differs from upstream with no cohere: comment among them. Each line
// is 1-based; for lines a copy dropped, oursLine is the line where they were.
type unmarkedChange struct {
	oursLine     int
	upstreamLine int
}

// unmarkedChanges is every run of lines where ours differs from upstream, along a longest common subsequence, that
// holds no cohere: comment. The walk takes a line both share as soon as it meets one, which keeps an inserted block
// in one run rather than splitting it where a brace inside it matches one of upstream's below.
func unmarkedChanges(ours []string, upstream []string) []unmarkedChange {
	// common[oursIndex*width+upstreamIndex] is the length of the longest common subsequence of ours[oursIndex:]
	// and upstream[upstreamIndex:].
	width := len(upstream) + 1
	common := make([]int32, (len(ours)+1)*width)
	for oursIndex := len(ours) - 1; oursIndex >= 0; oursIndex-- {
		for upstreamIndex := len(upstream) - 1; upstreamIndex >= 0; upstreamIndex-- {
			if ours[oursIndex] == upstream[upstreamIndex] {
				common[oursIndex*width+upstreamIndex] = common[(oursIndex+1)*width+upstreamIndex+1] + 1
				continue
			}
			common[oursIndex*width+upstreamIndex] = max(common[(oursIndex+1)*width+upstreamIndex],
				common[oursIndex*width+upstreamIndex+1])
		}
	}
	shared := func(oursIndex int, upstreamIndex int) bool {
		return oursIndex < len(ours) && upstreamIndex < len(upstream) && ours[oursIndex] == upstream[upstreamIndex]
	}

	var changes []unmarkedChange
	oursIndex, upstreamIndex := 0, 0
	for oursIndex < len(ours) || upstreamIndex < len(upstream) {
		if shared(oursIndex, upstreamIndex) {
			oursIndex++
			upstreamIndex++
			continue
		}
		change := unmarkedChange{oursLine: oursIndex + 1, upstreamLine: upstreamIndex + 1}
		marked := false
		for (oursIndex < len(ours) || upstreamIndex < len(upstream)) && !shared(oursIndex, upstreamIndex) {
			takesOurs := upstreamIndex == len(upstream) || oursIndex < len(ours) &&
				common[(oursIndex+1)*width+upstreamIndex] >= common[oursIndex*width+upstreamIndex+1]
			if !takesOurs {
				upstreamIndex++
				continue
			}
			marked = marked || isMarker(ours[oursIndex])
			oursIndex++
		}
		if !marked {
			changes = append(changes, change)
		}
	}
	return changes
}

// isMarker reports whether line is a comment that marks a change, which is any line comment with cohere: in it,
// the header's directives among them.
func isMarker(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "//") && strings.Contains(line, "cohere:")
}
