package return_nil_ness

import (
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// passFacts is the push walk's facts on a pass: ReturnedNilness facts, imported and exported as the original's are.
type passFacts struct {
	pass *analysis.Pass
}

func (facts passFacts) imported(function *types.Func) ([]ValueNilness, bool) {
	fact := new(ReturnedNilness)
	if !facts.pass.ImportObjectFact(function, fact) {
		return nil, false
	}
	return fact.Results, true
}

func (facts passFacts) record(function *types.Func, results []ValueNilness) {
	facts.pass.ExportObjectFact(function, &ReturnedNilness{Results: results})
}
