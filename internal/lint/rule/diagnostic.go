package rule

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/scanner"
)

// Diagnostic is one finding: a range, what is wrong there, and optionally how to repair it.
type Diagnostic struct {
	RuleName    string
	Range       core.TextRange
	Message     Message
	SourceFile  *ast.SourceFile
	Fixes       []Fix
	Suggestions []Suggestion
}

// Location is where a finding is, as every view of it names the place: the file's name, and the 1-based
// line and byte column of the range's start. Empty and zero for a finding with no file. cohere's own
// printers and the in-process runner (rule_runner) both read it, so the two name one place one way.
func (diagnostic Diagnostic) Location() (fileName string, line int, column int) {
	if diagnostic.SourceFile == nil {
		return "", 0, 0
	}
	line, character := scanner.GetECMALineAndByteOffsetOfPosition(diagnostic.SourceFile, diagnostic.Range.Pos())
	return diagnostic.SourceFile.FileName().AsString(), line + 1, character + 1
}
