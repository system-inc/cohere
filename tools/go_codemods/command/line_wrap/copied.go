package main

import (
	"go/ast"
	"strings"
)

// declaresCopy reports whether file opens a `cohere:copied` header in the comments before its package
// clause, the way internal/goengine/copied reads it. It answers true for a header copied.IsCopied would
// refuse as malformed too: a codemod leaves anything that claims to be upstream's alone, and the engine's
// own test names a malformed header.
func declaresCopy(file *ast.File) bool {
	for _, group := range file.Comments {
		if group.Pos() >= file.Package {
			return false
		}
		for _, comment := range group.List {
			if strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(comment.Text, "//")), "cohere:copied") {
				return true
			}
		}
	}
	return false
}
