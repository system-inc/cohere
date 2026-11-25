package printf

import (
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/analysis"
)

// operationSpan is operationRange's answer for an operation, placed only when a finding reads its Pos or End. Placing
// it unquotes the format literal from its start for each end of the span, and most operations report nothing, so
// placing every one up front was most of what checking an operation cost.
type operationSpan struct {
	formatArgument ast.Expr
	operation      *formatOperation

	// placed is operationRange's answer once Pos or End has asked for it, and nil before.
	placed analysis.Range
}

// Pos is where the operation's span starts.
func (span *operationSpan) Pos() token.Pos {
	return span.place().Pos()
}

// End is where the operation's span ends.
func (span *operationSpan) End() token.Pos {
	return span.place().End()
}

// place is operationRange's answer, found on the first call and kept for the second, since a report reads both ends.
func (span *operationSpan) place() analysis.Range {
	if span.placed == nil {
		span.placed = operationRange(span.formatArgument, span.operation)
	}
	return span.placed
}
