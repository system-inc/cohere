package function_purity

import (
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// passFacts is the push walk's facts on a pass: IsPure facts, imported and exported as the original's are.
type passFacts struct {
	pass *analysis.Pass
}

func (facts passFacts) holdsPure(function *types.Func) bool {
	return facts.pass.ImportObjectFact(function, new(IsPure))
}

func (facts passFacts) recordPure(function *types.Func) {
	facts.pass.ExportObjectFact(function, &IsPure{})
}
