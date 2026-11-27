package function_purity

import (
	"go/types"
	"maps"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_language/fact_resolver"
)

// NewPushAnalyzer is newPushAnalyzer, for the probes beside this file, which sit outside the package.
func NewPushAnalyzer() *analysis.Analyzer {
	return newPushAnalyzer()
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

// DefinitionAnalyzer is definitionAnalyzer, the definition's oracle, for the tests outside this package.
var DefinitionAnalyzer = definitionAnalyzer

// DefinitionHoldsPure is whether the definition's result on pass's package holds function pure, and whether pass holds
// that result at all.
func DefinitionHoldsPure(pass *analysis.Pass, function *types.Func) (bool, bool) {
	held, isHeld := pass.ResultOf[definitionAnalyzer].(definitionResult)
	if !isHeld {
		return false, false
	}
	return held[function], true
}

// Ask is ask, whether function is pure as a reader in reader holds it.
func Ask(resolver *fact_resolver.Resolver, reader *fact_resolver.Package, function *types.Func) bool {
	return ask(resolver, reader, function)
}

// Verdicts is every verdict purity's own state holds in home, by its function, read under home's lock: a copy.
func Verdicts(home *fact_resolver.Package) map[*types.Func]bool {
	home.Lock()
	defer home.Unlock()
	return maps.Clone(fact_resolver.StateOf[verdicts](home, Kind).pure)
}
