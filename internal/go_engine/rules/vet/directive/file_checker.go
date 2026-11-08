package directive

import (
	"go/ast"
	"go/token"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/tools/go/analysis"
)

// fileChecker checks the directive lines of one file, Go or not. x/tools' directive.checker.
type fileChecker struct {
	pass     *analysis.Pass
	filename string
	// file is the parsed Go file, and nil for a file that is not Go source.
	file *ast.File
	// inHeader holds while the lines read are above the package clause, or adjoin it.
	inHeader bool
}

// newFileChecker starts checking a file at its top, in its header.
func newFileChecker(pass *analysis.Pass, filename string, file *ast.File) *fileChecker {
	return &fileChecker{
		pass:     pass,
		filename: filename,
		file:     file,
		inHeader: true,
	}
}

// otherFile checks each line of a file that is not Go source, whose text begins at start in the file set, as far as
// its leading comments and blank lines go.
func (check *fileChecker) otherFile(start token.Pos, fullText string) {
	remaining := fullText
	inStarComment := false
	for remaining != "" {
		offset := len(fullText) - len(remaining)
		var line string
		line, remaining, _ = strings.Cut(remaining, "\n")

		if !inStarComment && strings.HasPrefix(line, "//") {
			check.comment(start+token.Pos(offset), line)
			continue
		}

		// Skip over and cut out any /* */ comments, so a // comment commented out by one is not read as a directive.
		for {
			line = strings.TrimSpace(line)
			if inStarComment {
				var closed bool
				_, line, closed = strings.Cut(line, "*/")
				if !closed {
					break
				}
				inStarComment = false
				continue
			}
			line, inStarComment = strings.CutPrefix(line, "/*")
			if !inStarComment {
				break
			}
		}
		if line != "" {
			// A line that is neither blank nor comment ends the space for valid //go:build comments, and also the part
			// of the file that can be read reliably without parsing it: past it, a "comment" could be inside a
			// multiline string, or the file might not be a program at all. So the check stops.
			break
		}
	}
}

// comment checks one line of a // comment, at position, its text beginning with the slashes.
func (check *fileChecker) comment(position token.Pos, line string) {
	if !strings.HasPrefix(line, "//go:") {
		return
	}
	// x/tools' testing hack, kept as the original has it: the Go repository's tests write `// ERROR` after a
	// directive, and the check stops there. It cannot change a finding, since the text it cuts begins with a space,
	// so the verb below always ends at or before the cut.
	directive, _, _ := strings.Cut(line, " // ERROR ")

	verb := directive
	if spaceIndex := strings.IndexFunc(directive, unicode.IsSpace); spaceIndex >= 0 {
		verb = directive[:spaceIndex]
		// A newline never ends a comment line read here, a Go comment's text holding none and another file's lines
		// being cut at each, and is accepted as the original accepts it.
		if directive[spaceIndex] != ' ' && directive[spaceIndex] != '\t' && directive[spaceIndex] != '\n' {
			space, _ := utf8.DecodeRuneInString(directive[spaceIndex:])
			check.pass.Reportf(position, "invalid space %#q in %s directive", space, verb)
		}
	}

	// Only //go:debug is judged. //go:build is the buildtag analyzer's, which reports a misplaced one, and every other
	// verb is left alone, since the original does not yet read the file's Go version to know which a toolchain takes.
	if verb != "//go:debug" {
		return
	}
	switch {
	case check.file == nil:
		check.pass.Reportf(position, "//go:debug directive only valid in Go source files")
	case check.file.Name.Name != "main" && !strings.HasSuffix(check.filename, "_test.go"):
		check.pass.Reportf(position, "//go:debug directive only valid in package main or test")
	case !check.inHeader:
		check.pass.Reportf(position, "//go:debug directive only valid before package declaration")
	}
}
