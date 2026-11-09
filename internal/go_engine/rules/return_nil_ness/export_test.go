package return_nil_ness

import (
	"go/types"
	"sync/atomic"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
)

// NewPushAnalyzer is newPushAnalyzer, for the differential beside this file, which runs it beside the reader that
// imports this package and so sits outside it.
func NewPushAnalyzer(judged *atomic.Int64) *analysis.Analyzer {
	return newPushAnalyzer(judged)
}

// PushNilness is the nilness push's result on pass's package holds of the result at index result of function, and
// whether pass holds push's result at all.
func PushNilness(pass *analysis.Pass, push *analysis.Analyzer, function *types.Func, result int) (ValueNilness, bool) {
	pushed, isPushed := pass.ResultOf[push].(*pushResult)
	if !isPushed {
		return ValueNilness{}, false
	}
	return pushed.Nilness(function, result), true
}

// Ask is ask, the nilness of function's results and whether a reader in reader holds them as a fact.
func Ask(resolver *fact_resolver.Resolver, reader *fact_resolver.Package, function *types.Func) ([]ValueNilness, bool) {
	return ask(resolver, reader, function)
}
