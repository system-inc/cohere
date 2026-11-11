package printf

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/type_predicates/verbs"
)

// matchArgumentType reports whether a verb that accepts accepted fits argument, and the reason when it does not and
// there is one to give, as verbs.Match judges the argument's type.
func matchArgumentType(pass *analysis.Pass, accepted verbs.Kinds, argument ast.Expr) (string, bool) {
	// %v and %T accept any argument.
	if accepted == verbs.Any {
		return "", true
	}
	passedType := pass.TypesInfo.Types[argument].Type
	if passedType == nil {
		return "", true // Probably a problem the type checker reports.
	}
	return verbs.Match(accepted, passedType)
}
