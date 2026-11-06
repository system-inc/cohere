package typescript

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/system-inc/cohere/internal/lint/ecmascript/comments"
)

// switchMetadata is the three facts about a switch that all three checks read, computed once per
// statement.
//
// Upstream names this `SwitchMetadata` and exports it. Nothing outside the rule reads it, so it is
// unexported here, and that forces the second half of the same rename: upstream's three check
// closures each take a parameter also named `switchMetadata`, shadowing the type inside their own
// bodies, which compiles only while the type's name differs in case. Those parameters are `metadata`
// here. Both renames are naming rather than behavior (no expression changed, only what it is
// spelled), and they are the only edits in this file beyond the interface renames and the guard.
type switchMetadata struct {
	ContainsNonLiteralType bool
	// nil if there is no default case
	DefaultCase *ast.CaseOrDefaultClause
	// DefaultCaseComment is the comment standing in for a default clause, when there is no clause and
	// the last comment after the last case matches the pattern; nil otherwise.
	DefaultCaseComment *comments.Comment
	// DefaultCaseCommentOverran is a DefaultCaseComment taken because its pattern's match overran the time
	// bound (esregexp.MatchTimeout) rather than matched: it spares a missing default, and is not reported.
	DefaultCaseCommentOverran bool
	MissingLiteralBranchTypes []*checker.Type
	// Upstream's `SymbolName` field is left out: only a fix reads it, and this rule ships none.
}

// hasDefaultCase is upstream's `defaultCase != null`, which a matching comment satisfies too.
func (metadata *switchMetadata) hasDefaultCase() bool {
	return metadata.DefaultCase != nil || metadata.DefaultCaseComment != nil
}
