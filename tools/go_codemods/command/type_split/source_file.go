package main

import (
	"bytes"
	"go/ast"
	"go/token"
)

// sourceFile is one Go file of a package group, parsed with comments and object resolution.
type sourceFile struct {
	name      string
	source    []byte
	syntax    *ast.File
	tokenFile *token.File
	// key is everything that decides whether the go command compiles this file alongside another:
	// test or not, the file name's `GOOS` and `GOARCH` suffix, and the //go:build line. Code moves only
	// between files with the same key.
	key string
	// buildLines are the //go:build and // +build lines above the package clause, copied to new files.
	buildLines []string
	// pinned says why nothing moves into or out of this file, or is empty.
	pinned string
	// built is whether the build the rules run under compiles this file, so its methods make a type count.
	built bool
	// judged is whether the one type per file and file name rules report in this file: a built file that is
	// neither generated nor a declared copy of upstream source, whose findings the engine withholds.
	judged bool
	// importNames maps each import's bound name to its import spec, blank and dot imports excluded.
	importNames map[string]*ast.ImportSpec
}

func (file *sourceFile) line(position token.Pos) int {
	return file.tokenFile.Line(position)
}

func (file *sourceFile) offset(position token.Pos) int {
	return file.tokenFile.Offset(position)
}

// trailingEnd extends an end offset over comments that start on the same line after it.
func (file *sourceFile) trailingEnd(end int) int {
	extended := end
	for _, group := range file.syntax.Comments {
		start := file.offset(group.Pos())
		if start >= extended && !bytes.Contains(file.source[extended:start], []byte("\n")) {
			extended = file.offset(group.End())
		}
	}
	return extended
}

// lineRange is the byte range of a declaration from start to end, widened to whole lines: the
// indentation before it, a comment after it on its last line, and the newline that ends it.
func (file *sourceFile) lineRange(start token.Pos, end token.Pos) byteRange {
	source := file.source
	from := file.offset(start)
	to := file.trailingEnd(file.offset(end))
	lineStart := from
	for lineStart > 0 && (source[lineStart-1] == ' ' || source[lineStart-1] == '\t') {
		lineStart--
	}
	if lineStart == 0 || source[lineStart-1] == '\n' {
		from = lineStart
	}
	lineEnd := to
	for lineEnd < len(source) && (source[lineEnd] == ' ' || source[lineEnd] == '\t' || source[lineEnd] == '\r') {
		lineEnd++
	}
	if lineEnd == len(source) {
		to = lineEnd
	} else if source[lineEnd] == '\n' {
		to = lineEnd + 1
	}
	return byteRange{start: from, end: to}
}

func (file *sourceFile) text(start token.Pos, end token.Pos) []byte {
	return file.source[file.offset(start):file.offset(end)]
}
