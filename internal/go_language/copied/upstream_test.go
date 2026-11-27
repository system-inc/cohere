package copied

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// A file is a copy only while it is one. This holds every file that declares itself a copy to the upstream text
// recorded for it in testdata/upstream: each run of lines where the two differ holds a cohere: comment, and every
// other line is upstream's, byte for byte. It is a test rather than a lint rule because it reads upstream's text,
// which cohere's binary does not carry. The text is recorded once, from the module cache and a Go distribution's
// source, by tools/generate, so the test reads only the repository and starts no process; SHA256SUMS holds each
// recorded file's digest, so an edit to upstream's text fails as surely as an unmarked edit to a copy. A copy with no
// recorded text fails, never skips, since a skipped check reads as a copy that matched. What keeps the toolchain on
// a std copy's release is the copy's own package's test (load/build's TestTheToolchainsGoBuildIsTheCopys), since
// only it knows which releases ship the sources it copied.

// skippedDirectories are what the walk leaves out: the TypeScript submodule, which is upstream's own tree rather
// than a copy in ours; npm's node_modules; and testdata, whose planted copies TestPlantedCopies reads and whose
// recorded upstream text declares no copy.
var skippedDirectories = []string{"TypeScript", "node_modules", "testdata"}

// recordedDirectory holds the upstream text tools/generate recorded: each file at <module>@<version>/<path>, each
// source's LICENSE beside its files, and digestsFile.
const recordedDirectory = "testdata/upstream"

// digestsFile is the recorded files' sha256 sums, one `<hex>  <path>` line each as sha256sum writes them, and
// readmeFile says what the directory is; neither is recorded text.
const (
	digestsFile = "SHA256SUMS"
	readmeFile  = "README.md"
)

// TestEveryCopyMatchesUpstreamOutsideItsMarkedChanges walks the module for every Go file outside testdata that
// declares itself a copy and holds each to its recorded upstream file. Every recorded file is one a copy names, or
// its source's LICENSE, so text left behind by a copy bumped or deleted fails rather than lingering.
func TestEveryCopyMatchesUpstreamOutsideItsMarkedChanges(test *testing.T) {
	test.Parallel()

	upstream, recordedError := readRecorded(os.DirFS(recordedDirectory))
	if recordedError != nil {
		test.Fatal(recordedError)
	}
	// This package sits three directories below the module root.
	root, rootError := filepath.Abs(filepath.Join("..", "..", ".."))
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
		header, declared, problems := checkCopy(upstream, filepath.ToSlash(relative), contents)
		if declared {
			copies = append(copies, filepath.ToSlash(relative))
			named[recordedPath(header)] = true
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
	for _, recorded := range slices.Sorted(maps.Keys(upstream.digests)) {
		if !strings.HasSuffix(recorded, "/LICENSE") && !named[recorded] {
			test.Errorf("%s/%s is recorded, but no copy names it: delete it and its line in %s", recordedDirectory,
				recorded, digestsFile)
		}
	}
}

// TestEveryRecordedFileMatchesItsDigest holds testdata/upstream to SHA256SUMS: every file listed is there with the
// digest listed, every file there is listed, and every source holds its LICENSE.
func TestEveryRecordedFileMatchesItsDigest(test *testing.T) {
	test.Parallel()
	upstream, recordedError := readRecorded(os.DirFS(recordedDirectory))
	if recordedError != nil {
		test.Fatal(recordedError)
	}
	for _, problem := range upstream.verify() {
		test.Error(problem)
	}
}

// TestPlantedCopies holds the check to both answers on planted copies of x/tools' driverutil/url.go: a faithful
// copy with a marked change passes, and an unmarked edit, a header with no until and a module with no recorded text
// each fail, naming what failed.
func TestPlantedCopies(test *testing.T) {
	test.Parallel()
	upstream, recordedError := readRecorded(os.DirFS(recordedDirectory))
	if recordedError != nil {
		test.Fatal(recordedError)
	}
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
		{file: "unrecorded/url.go", saying: []string{"unrecorded/url.go: no upstream text is recorded for " +
			"example.com/unrecorded@v1.0.0 url.go"}},
	} {
		test.Run(planted.file, func(test *testing.T) {
			test.Parallel()
			contents, readError := os.ReadFile(filepath.Join("testdata", filepath.FromSlash(planted.file)))
			if readError != nil {
				test.Fatalf("reading the planted copy: %v", readError)
			}
			_, declared, problems := checkCopy(upstream, planted.file, contents)
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

// TestPlantedRecordings holds the digest check to both answers on planted recorded trees: a faithful one passes,
// and text edited past its digest, a listed file missing, a file not listed, a source with no LICENSE and a
// malformed SHA256SUMS each fail, naming what failed. A copy held to edited text fails too, before any diff.
func TestPlantedRecordings(test *testing.T) {
	test.Parallel()
	const upstreamText = "package a\n"
	sum := sha256.Sum256([]byte(upstreamText))
	digest := hex.EncodeToString(sum[:])
	license := sha256.Sum256([]byte("BSD\n"))
	licensed := hex.EncodeToString(license[:]) + "  example.com/a@v1.0.0/LICENSE\n"
	for _, planted := range []struct {
		name  string
		files fstest.MapFS

		// saying are what the problems say, one each, in order; none for a tree that passes. readSaying is what
		// reading SHA256SUMS says, for a tree that cannot be read at all.
		saying     []string
		readSaying string
	}{
		{name: "faithful", files: fstest.MapFS{
			digestsFile:                    {Data: []byte(licensed + digest + "  example.com/a@v1.0.0/a.go\n")},
			readmeFile:                     {Data: []byte("# planted\n")},
			"example.com/a@v1.0.0/LICENSE": {Data: []byte("BSD\n")},
			"example.com/a@v1.0.0/a.go":    {Data: []byte(upstreamText)},
		}},
		{name: "edited past its digest", files: fstest.MapFS{
			digestsFile:                    {Data: []byte(licensed + digest + "  example.com/a@v1.0.0/a.go\n")},
			"example.com/a@v1.0.0/LICENSE": {Data: []byte("BSD\n")},
			"example.com/a@v1.0.0/a.go":    {Data: []byte("package b\n")},
		}, saying: []string{"testdata/upstream/example.com/a@v1.0.0/a.go: its sha256 is "}},
		{name: "a listed file missing", files: fstest.MapFS{
			digestsFile:                    {Data: []byte(licensed + digest + "  example.com/a@v1.0.0/a.go\n")},
			"example.com/a@v1.0.0/LICENSE": {Data: []byte("BSD\n")},
		}, saying: []string{"testdata/upstream/example.com/a@v1.0.0/a.go: reading the recorded text: "}},
		{name: "a file not listed", files: fstest.MapFS{
			digestsFile:                    {Data: []byte(licensed + digest + "  example.com/a@v1.0.0/a.go\n")},
			"example.com/a@v1.0.0/LICENSE": {Data: []byte("BSD\n")},
			"example.com/a@v1.0.0/a.go":    {Data: []byte(upstreamText)},
			"example.com/a@v1.0.0/b.go":    {Data: []byte(upstreamText)},
		}, saying: []string{"testdata/upstream/example.com/a@v1.0.0/b.go: no line of SHA256SUMS lists it"}},
		{name: "a source with no LICENSE", files: fstest.MapFS{
			digestsFile:                 {Data: []byte(digest + "  example.com/a@v1.0.0/a.go\n")},
			"example.com/a@v1.0.0/a.go": {Data: []byte(upstreamText)},
		}, saying: []string{"testdata/upstream/example.com/a@v1.0.0: no LICENSE is recorded beside its files"}},
		{name: "a malformed line", files: fstest.MapFS{
			digestsFile: {Data: []byte(digest + " example.com/a@v1.0.0/a.go\n")},
		}, readSaying: "testdata/upstream/SHA256SUMS:1: "},
		{name: "a path listed twice", files: fstest.MapFS{
			digestsFile: {Data: []byte(licensed + licensed)},
		}, readSaying: "testdata/upstream/SHA256SUMS:2: lists example.com/a@v1.0.0/LICENSE a second time"},
	} {
		test.Run(planted.name, func(test *testing.T) {
			test.Parallel()
			upstream, recordedError := readRecorded(planted.files)
			if planted.readSaying != "" {
				if recordedError == nil || !strings.HasPrefix(recordedError.Error(), planted.readSaying) {
					test.Fatalf("reading SHA256SUMS: got %v, want an error opening with %s", recordedError,
						planted.readSaying)
				}
				return
			}
			if recordedError != nil {
				test.Fatal(recordedError)
			}
			problems := upstream.verify()
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

	// A copy is held to recorded text only once the text is its digest's, so an edit to both at once cannot pass.
	upstream, recordedError := readRecorded(fstest.MapFS{
		digestsFile:                 {Data: []byte(digest + "  example.com/a@v1.0.0/a.go\n")},
		"example.com/a@v1.0.0/a.go": {Data: []byte("package b\n")},
	})
	if recordedError != nil {
		test.Fatal(recordedError)
	}
	copyText := "// cohere:copied example.com/a@v1.0.0 a.go\n// cohere:until never: planted.\n\npackage b\n"
	_, declared, problems := checkCopy(upstream, "planted/a.go", []byte(copyText))
	if !declared || len(problems) != 1 || !strings.Contains(problems[0], "its sha256 is ") {
		test.Errorf("a copy held to text edited past its digest: declared %t, problems %q, want one naming the "+
			"digest", declared, problems)
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

// checkCopy reads the header of the file at path, its contents given, and holds a declared copy to its recorded
// upstream text. It returns the header, whether the file declares a copy, and every problem as a sentence naming the
// file and, for a line that differs, the line and upstream's path.
func checkCopy(upstream recorded, path string, contents []byte) (Header, bool, []string) {
	if !bytes.Contains(contents, []byte(copiedDirective)) && !bytes.Contains(contents, []byte(untilDirective)) {
		return Header{}, false, nil
	}
	file, parseError := parser.ParseFile(token.NewFileSet(), path, contents,
		parser.PackageClauseOnly|parser.ParseComments)
	if parseError != nil {
		return Header{}, true, []string{fmt.Sprintf("%s: parsing it for a copy header: %v", path, parseError)}
	}
	header, declared, readError := Read(file)
	if !declared {
		return Header{}, false, nil
	}
	if readError != nil {
		return header, true, []string{fmt.Sprintf("%s: %v", path, readError)}
	}
	if _, listed := upstream.digests[recordedPath(header)]; !listed {
		return header, true, []string{fmt.Sprintf("%s: no upstream text is recorded for %s@%s %s in %s; "+
			"tools/generate records it", path, header.Module, header.Version, header.Path, recordedDirectory)}
	}
	upstreamText, textError := upstream.text(recordedPath(header))
	if textError != nil {
		return header, true, []string{fmt.Sprintf("%s: %v", path, textError)}
	}
	var problems []string
	ourLines, upstreamLines := strings.Split(string(contents), "\n"), strings.Split(string(upstreamText), "\n")
	for _, change := range unmarkedChanges(ourLines, upstreamLines) {
		problems = append(problems, fmt.Sprintf("%s:%d: differs from %s@%s %s:%d with no cohere: comment among "+
			"the lines that differ", path, change.oursLine, header.Module, header.Version, header.Path,
			change.upstreamLine))
	}
	return header, true, problems
}

// recordedPath is where recordedDirectory holds the upstream file header names, as tools/generate writes it.
func recordedPath(header Header) string {
	return header.Module + "@" + header.Version + "/" + header.Path
}

// recorded is the upstream text in a recorded tree, with each file's digest as SHA256SUMS lists it, by its path
// from the tree's root.
type recorded struct {
	files   fs.FS
	digests map[string]string
}

// readRecorded reads files' SHA256SUMS. Each line is a lowercase hex sha256, two spaces and a clean slash-separated
// path, listed once.
func readRecorded(files fs.FS) (recorded, error) {
	sums, readError := fs.ReadFile(files, digestsFile)
	if readError != nil {
		return recorded{}, fmt.Errorf("reading %s/%s: %w", recordedDirectory, digestsFile, readError)
	}
	upstream := recorded{files: files, digests: map[string]string{}}
	lines := bufio.NewScanner(bytes.NewReader(sums))
	for number := 1; lines.Scan(); number++ {
		digest, listedPath, spelled := strings.Cut(lines.Text(), "  ")
		decoded, decodeError := hex.DecodeString(digest)
		if !spelled || decodeError != nil || len(decoded) != sha256.Size || hex.EncodeToString(decoded) != digest ||
			!fs.ValidPath(listedPath) || listedPath == "." {
			return recorded{}, fmt.Errorf("%s/%s:%d: %q is not spelled <lowercase hex sha256>  <path>",
				recordedDirectory, digestsFile, number, lines.Text())
		}
		if _, listed := upstream.digests[listedPath]; listed {
			return recorded{}, fmt.Errorf("%s/%s:%d: lists %s a second time", recordedDirectory, digestsFile,
				number, listedPath)
		}
		upstream.digests[listedPath] = digest
	}
	if scanError := lines.Err(); scanError != nil {
		return recorded{}, fmt.Errorf("reading %s/%s: %w", recordedDirectory, digestsFile, scanError)
	}
	return upstream, nil
}

// text is the recorded file at listedPath, which must be listed, once its digest is the one SHA256SUMS lists, so
// text edited by hand is never what a copy is held to.
func (upstream recorded) text(listedPath string) ([]byte, error) {
	contents, readError := fs.ReadFile(upstream.files, listedPath)
	if readError != nil {
		return nil, fmt.Errorf("%s/%s: reading the recorded text: %w", recordedDirectory, listedPath, readError)
	}
	sum := sha256.Sum256(contents)
	if digest := hex.EncodeToString(sum[:]); digest != upstream.digests[listedPath] {
		return nil, fmt.Errorf("%s/%s: its sha256 is %s, but %s lists %s: recorded text is upstream's and never "+
			"edited, so record it again with tools/generate", recordedDirectory, listedPath, digest, digestsFile,
			upstream.digests[listedPath])
	}
	return contents, nil
}

// verify is every problem with the recorded tree, in order of path: a listed file missing or whose digest is not
// the one listed, a file no line lists, and a source, the first directory below the module's, with no LICENSE.
func (upstream recorded) verify() []string {
	var problems []string
	for _, listedPath := range slices.Sorted(maps.Keys(upstream.digests)) {
		if _, textError := upstream.text(listedPath); textError != nil {
			problems = append(problems, textError.Error())
		}
	}
	walkError := fs.WalkDir(upstream.files, ".", func(walkedPath string, entry fs.DirEntry, pathError error) error {
		if pathError != nil {
			return pathError
		}
		if entry.IsDir() || walkedPath == digestsFile || walkedPath == readmeFile {
			return nil
		}
		if _, listed := upstream.digests[walkedPath]; !listed {
			problems = append(problems, fmt.Sprintf("%s/%s: no line of %s lists it, so no digest holds it",
				recordedDirectory, walkedPath, digestsFile))
		}
		return nil
	})
	if walkError != nil {
		problems = append(problems, fmt.Sprintf("walking %s: %v", recordedDirectory, walkError))
	}
	sources := map[string]bool{}
	for listedPath := range upstream.digests {
		sources[source(listedPath)] = true
	}
	for _, listedSource := range slices.Sorted(maps.Keys(sources)) {
		if _, licensed := upstream.digests[listedSource+"/LICENSE"]; !licensed {
			problems = append(problems, fmt.Sprintf("%s/%s: no LICENSE is recorded beside its files, as its "+
				"license asks", recordedDirectory, listedSource))
		}
	}
	return problems
}

// source is the <module>@<version> directory listedPath is below, which is the first one whose name holds an @,
// since a module path's own elements never do.
func source(listedPath string) string {
	elements := strings.Split(listedPath, "/")
	for index, element := range elements[:len(elements)-1] {
		if strings.Contains(element, "@") {
			return strings.Join(elements[:index+1], "/")
		}
	}
	return strings.Join(elements[:len(elements)-1], "/")
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
