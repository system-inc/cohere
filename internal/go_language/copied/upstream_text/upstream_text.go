// Package upstream_text holds a copy to the upstream text recorded for it, for the tests that keep every copy one.
//
// A file is a copy only while it is one (internal/go_language/copied): each run of lines where it and upstream's
// file differ holds a cohere: comment, and every other line is upstream's, byte for byte. This is a test's check
// rather than a lint rule because it reads upstream's text, which cohere's binary does not carry. The text is
// recorded once, from the module cache and a Go distribution's source, into testdata/upstream by tools/generate, so
// the check reads only the repository and starts no process; SHA256SUMS holds each recorded file's digest, so an
// edit to upstream's text fails as surely as an unmarked edit to a copy.
//
// The package's own tests hold the check to both answers on planted copies and recordings, and the recorded tree to
// its digests, reading only this package's testdata. The walk that holds every copy in the module to its text is
// internal/go_language/copied/every_copy's, kept apart because it reads every Go file in the module, so Go's test
// cache reruns it on any change while these tests replay.
package upstream_text

import (
	"bytes"
	"fmt"
	"go/parser"
	"go/token"
	"strings"

	"github.com/system-inc/cohere/internal/go_language/copied"
)

// RecordedDirectory holds the upstream text tools/generate recorded, from this package's directory: each file at
// <module>@<version>/<path>, each source's LICENSE beside its files, and digestsFile.
const RecordedDirectory = "testdata/upstream"

// Check reads the header of the file at path, its contents given, and holds a declared copy to its recorded
// upstream text. It returns the header, whether the file declares a copy, and every problem as a sentence naming the
// file and, for a line that differs, the line and upstream's path.
func Check(upstream Recorded, path string, contents []byte) (copied.Header, bool, []string) {
	// A file that holds neither of the header's two directives, as copied spells them, declares nothing and need not
	// be parsed.
	if !bytes.Contains(contents, []byte("cohere:copied")) && !bytes.Contains(contents, []byte("cohere:until")) {
		return copied.Header{}, false, nil
	}
	file, parseError := parser.ParseFile(token.NewFileSet(), path, contents,
		parser.PackageClauseOnly|parser.ParseComments)
	if parseError != nil {
		return copied.Header{}, true, []string{fmt.Sprintf("%s: parsing it for a copy header: %v", path, parseError)}
	}
	header, declared, readError := copied.Read(file)
	if !declared {
		return copied.Header{}, false, nil
	}
	if readError != nil {
		return header, true, []string{fmt.Sprintf("%s: %v", path, readError)}
	}
	if _, listed := upstream.digests[RecordedPath(header)]; !listed {
		return header, true, []string{fmt.Sprintf("%s: no upstream text is recorded for %s@%s %s in %s; "+
			"tools/generate records it", path, header.Module, header.Version, header.Path, RecordedDirectory)}
	}
	upstreamText, textError := upstream.text(RecordedPath(header))
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

// RecordedPath is where RecordedDirectory holds the upstream file header names, as tools/generate writes it.
func RecordedPath(header copied.Header) string {
	return header.Module + "@" + header.Version + "/" + header.Path
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
