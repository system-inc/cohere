// Package hoisted_map_key_conversion is staticcheck's SA6001, a conversion of a byte slice to a string stored in a
// variable and read only as the key of map lookups, where converting inside each lookup would let the compiler skip
// the copy, ported onto the engine's own intermediate representation (#tf4cb8g).
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa6001/sa6001.go, whose logic it keeps and
// whose message it reports word for word, from the start to the end of the conversion, where staticcheck's report
// puts it. It reads a conversion's type set through honnef.co/go/tools/go/types/typeutil, as the original does, so a
// type parameter whose set holds a byte slice counts as one. The original walks every source function of a package in
// one run, but keeps nothing from one function to the next: each conversion is judged by its own readers. So the port
// judges one function at a time and finds what the original finds. The registry runs the port in the original's place,
// under its name, and the original stays only as the oracle: the parity tests beside this file run both over the same
// packages, and they differ nowhere. The one place the port could differ is a conversion with no syntax, where
// staticcheck's report panics and the port reports nothing; staticcheck builds every conversion a map lookup reads from
// its call expression, so no such conversion reaches either.
package hoisted_map_key_conversion

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// Listener is what the port hears of each package from the dispatcher: every function, each judged on its own.
var Listener = rule.Listener{
	FileLocal:                  true,
	IntermediateRepresentation: &rule.IntermediateRepresentationListener{Function: Check},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA6001",
	"SA6001",
	"Flags a byte slice converted to a string ahead of the map lookups that are its only readers: staticcheck's "+
		"SA6001, ported onto the engine's own intermediate representation.",
	&Listener,
)

// message is the original's, word for word.
const message = "m[string(key)] would be more efficient than k := string(key); m[k]"

// Check judges one function: each conversion to string of a value whose type, or one of whose type set's terms, is a
// byte slice, reported when map lookups written with a bare name for their key are all that read it.
func Check(context *rule.Context, function *intermediate_representation.Function) {
	for _, block := range function.Blocks {
		for _, instruction := range block.Instructions {
			var conversion intermediate_representation.Value
			var fromType types.Type
			switch instruction := instruction.(type) {
			case *intermediate_representation.Convert:
				conversion = instruction
				fromType = instruction.X.Type()
			case *intermediate_representation.MultiConvert:
				// A conversion from a type parameter, one conversion per term of its type set.
				conversion = instruction
				fromType = instruction.X.Type()
			default:
				continue
			}
			// The predeclared string alone, by identity, as the original compares: a named string type, or a type
			// parameter whose set holds string, is no string here.
			if conversion.Type() != types.Universe.Lookup("string").Type() || !holdsByteSlice(fromType) {
				continue
			}
			if !isReadOnlyByNamedLookups(conversion) {
				continue
			}
			source := conversion.Source()
			if source == nil {
				// A conversion with no syntax has no place to report; staticcheck's report panics on one.
				continue
			}
			context.Report(analysis.Diagnostic{Pos: source.Pos(), End: source.End(), Message: message})
		}
	}
}

// holdsByteSlice is whether any term of fromType's type set has a slice of byte as its underlying type: a type that
// is one has itself as its one term, and a type parameter whose set holds one converts it too.
func holdsByteSlice(fromType types.Type) bool {
	return typeutil.NewTypeSet(fromType).Any(func(term *types.Term) bool {
		slice, isSlice := term.Type().Underlying().(*types.Slice)
		return isSlice && slice.Elem().Underlying() == types.Universe.Lookup("byte").Type()
	})
}

// isReadOnlyByNamedLookups is whether conversion has readers and every one is a map lookup whose syntax indexes the
// map by a bare name, the variable the conversion was stored in. A lookup written another way, or a reader of any
// other kind, a call, a map update, a comparison, means the string is not one only map lookups use.
func isReadOnlyByNamedLookups(conversion intermediate_representation.Value) bool {
	referrers := conversion.Referrers()
	if referrers == nil || len(*referrers) == 0 {
		return false
	}
	for _, referrer := range *referrers {
		if _, isLookup := referrer.(*intermediate_representation.MapLookup); !isLookup {
			return false
		}
		index, isIndex := referrer.Source().(*ast.IndexExpr)
		if !isIndex {
			return false
		}
		if _, isName := index.Index.(*ast.Ident); !isName {
			return false
		}
	}
	return true
}
