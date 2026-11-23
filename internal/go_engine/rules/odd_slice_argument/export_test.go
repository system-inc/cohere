package odd_slice_argument

import (
	"sync/atomic"

	"github.com/system-inc/go-types/types"
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

// IsDefinitionMark is whether fact is the definition's mark.
func IsDefinitionMark(fact analysis.Fact) bool {
	_, isMark := fact.(*evenByDefinition)
	return isMark
}

// Whole is whole, every parameter of home marked as the resolver computes a package whole for the store.
func Whole(resolver *fact_resolver.Resolver, home *fact_resolver.Package) map[types.Object]bool {
	return whole(resolver, home)
}

// ParameterOwner is parameterOwner, where a parameter is declared, for the tests outside this package.
type ParameterOwner = parameterOwner

// IsLiteral is whether owner is a function literal.
func IsLiteral(owner ParameterOwner) bool {
	return owner.literal != nil
}

// IsAtLoad is whether owner is a function literal at package level, which runs in the package's initializer.
func IsAtLoad(owner ParameterOwner) bool {
	return owner.literal != nil && owner.declared == nil
}

// OwnerOf is ownerOf, where parameter is declared in home, nil for nowhere.
func OwnerOf(home *fact_resolver.Package, parameter *types.Var) *ParameterOwner {
	return ownerOf(home, parameter)
}

// OwnersByWalk is ownersByWalk, every named parameter of home's functions and function literals by where it is
// declared, read from all of home's syntax, the oracle OwnerOf is held to.
func OwnersByWalk(home *fact_resolver.Package) map[*types.Var]ParameterOwner {
	return ownersByWalk(home)
}

// SettledMarks is the mark of every parameter of every function SA5012's own state holds settled in home, marked or
// not, by the parameter, read under home's lock.
func SettledMarks(home *fact_resolver.Package) map[*types.Var]bool {
	home.Lock()
	defer home.Unlock()
	settled := map[*types.Var]bool{}
	for function, marked := range fact_resolver.StateOf[marks](home, Kind).functions {
		for parameter := range ownParameters(function).Variables() {
			settled[parameter] = marked[parameter]
		}
	}
	return settled
}
