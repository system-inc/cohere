package odd_slice_argument

import (
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// passFacts is the push walk's marks on a pass: evenElements facts, imported and exported as the original's are.
type passFacts struct {
	pass *analysis.Pass
}

func (marks passFacts) marked(parameter *types.Var) bool {
	return marks.pass.ImportObjectFact(parameter, new(evenElements))
}

func (marks passFacts) mark(parameter types.Object) {
	marks.pass.ExportObjectFact(parameter, new(evenElements))
}
