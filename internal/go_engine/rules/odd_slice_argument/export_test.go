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

// DefinitionAnalyzer is definitionAnalyzer, the definition's oracle, for the tests outside this package.
var DefinitionAnalyzer = definitionAnalyzer

// Definition is every parameter the definition's result on pass's package holds marked, its own and its imports' that
// the facts carried there, and whether pass holds that result at all.
func Definition(pass *analysis.Pass) (map[types.Object]bool, bool) {
	defined, isDefined := pass.ResultOf[definitionAnalyzer].(definitionResult)
	return defined, isDefined
}

// OwnMarksAnalyzer is ownMarksAnalyzer, the marks the analyzer makes of the package it runs on, for the tests outside
// this package.
var OwnMarksAnalyzer = ownMarksAnalyzer

// OwnMarks is every parameter the analyzer's own marks hold on pass's package, and whether pass holds them at all.
func OwnMarks(pass *analysis.Pass) (map[types.Object]bool, bool) {
	own, isOwn := pass.ResultOf[ownMarksAnalyzer].(ownMarksResult)
	return own, isOwn
}

// Whole is whole, every parameter of home marked as the resolver computes a package whole for the store.
func Whole(resolver *fact_resolver.Resolver, home *fact_resolver.Package) map[types.Object]bool {
	return whole(resolver, home)
}
