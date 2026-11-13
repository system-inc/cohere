package function_purity

import (
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// definitionCallees answers judge's question from the set so far, and from the facts for another package's function.
type definitionCallees struct {
	pass *analysis.Pass
	pure map[*types.Func]bool
}

func (callees definitionCallees) isPure(function *intermediate_representation.Function) bool {
	if function.Object() == nil {
		return false
	}
	declared := declaredFunction(function)
	if declared.Pkg() == callees.pass.Pkg {
		return callees.pure[declared]
	}
	return callees.pass.ImportObjectFact(declared, new(IsPureByDefinition))
}
