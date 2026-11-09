package odd_slice_argument

import (
	"go/types"
	"sync/atomic"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
)

// NewPushAnalyzer is newPushAnalyzer, for the differential beside this file, which sits outside the package.
func NewPushAnalyzer(judged *atomic.Int64) *analysis.Analyzer {
	return newPushAnalyzer(judged)
}

// PushMarked is whether push's result on pass's package holds parameter marked, and whether pass holds push's result
// at all.
func PushMarked(pass *analysis.Pass, push *analysis.Analyzer, parameter *types.Var) (bool, bool) {
	pushed, isPushed := pass.ResultOf[push].(pushResult)
	if !isPushed {
		return false, false
	}
	return pushed[parameter], true
}

// IsMark is whether fact is the push's mark.
func IsMark(fact analysis.Fact) bool {
	_, isMark := fact.(*evenElements)
	return isMark
}

// Ask is ask, whether parameter is marked as a reader in reader holds it.
func Ask(resolver *fact_resolver.Resolver, reader *fact_resolver.Package, parameter *types.Var) bool {
	return ask(resolver, reader, parameter)
}
