// Package core_type answers a type's core type as honnef.co/go/tools' typeutil.CoreType does, without its term set
// for a type that can have only one term. typeutil.CoreType computes the term set through typeparams.NormalTerms on
// every call, a map, a term list and a term, and almost every type our IR builder and rules ask about is no type
// parameter, interface or union, whose term set is the single term of the type itself (#dmjqz1f, #bq6gxwy's cut 6:
// about 0.19 CPU-s of a cold run at cohere's root). It is ours and not a change to staticcheck's copy, so the
// originals that stay as parity's oracles keep upstream's answer to hold this one to.
package core_type

import (
	"go/types"

	"honnef.co/go/tools/go/types/typeutil"
)

// Of is typ's core type, or nil when it has none, as typeutil.CoreType answers. A type that is no type parameter and
// whose underlying type is no interface or union has the term set {typ}, so its core type is its underlying type, or
// none when that is the invalid type, whose term set is empty. Every other type, a type parameter among them even
// when its constraint isn't set yet, is answered by typeutil.CoreType itself.
func Of(typ types.Type) types.Type {
	if _, isTypeParameter := typ.(*types.TypeParam); !isTypeParameter && typ != nil {
		switch underlying := typ.Underlying().(type) {
		case *types.Interface, *types.Union:
		default:
			if underlying == types.Typ[types.Invalid] {
				return nil
			}
			return underlying
		}
	}
	return typeutil.CoreType(typ)
}
