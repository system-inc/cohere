package function_purity

import (
	"go/types"
	"sync/atomic"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
)

// NewPushAnalyzer is newPushAnalyzer, for the differential beside this file, which runs it beside the readers that
// import this package and so sits outside it.
func NewPushAnalyzer(judged *atomic.Int64) *analysis.Analyzer {
	return newPushAnalyzer(judged)
}

// PushHoldsPure is whether push's result on pass's package holds function pure, and whether pass holds push's result
// at all.
func PushHoldsPure(pass *analysis.Pass, push *analysis.Analyzer, function *types.Func) (bool, bool) {
	pushed, isPushed := pass.ResultOf[push].(pushResult)
	if !isPushed {
		return false, false
	}
	_, holds := pushed[function]
	return holds, true
}

// Ask is ask, whether function is pure as a reader in reader holds it.
func Ask(resolver *fact_resolver.Resolver, reader *fact_resolver.Package, function *types.Func) bool {
	return ask(resolver, reader, function)
}
