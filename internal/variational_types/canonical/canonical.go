// Package canonical spells a type of the variational fork as the platform census spells a go/types type
// (command/platform_census_throwaway/loaded_build.go, copied whole for the fork's types), so the spike's projection is
// compared exactly as the census oracle compares: a signature without names, an alias as its target, byte as uint8,
// every package by its path (#yraxcp9).
package canonical

import (
	"strconv"
	"strings"

	"github.com/system-inc/cohere/internal/variational_types/types"
)

// Spell is typed's canonical spelling, or `<nil>` for no type.
func Spell(typed types.Type) string {
	if typed == nil {
		return "<nil>"
	}
	var builder strings.Builder
	writeCanonicalType(&builder, typed)
	return builder.String()
}

// Answer is what the census compares of an expression: its type, and its constant value when it has one.
func Answer(typeAndValue types.TypeAndValue) string {
	answer := Spell(typeAndValue.Type)
	if typeAndValue.Value != nil {
		answer += " = " + typeAndValue.Value.ExactString()
	}
	return answer
}

// channelOpenings begins a channel type's spelling by its direction, its element in parentheses so `chan (<-chan int)`
// and `chan<- chan int` never spell alike.
var channelOpenings = [...]string{types.SendRecv: "chan(", types.SendOnly: "chan<-(", types.RecvOnly: "<-chan("}

// writeCanonicalType writes typed as typeString spells it. A named type is its path, name and type arguments, never its
// underlying type, so the walk ends however a type refers to itself.
//
// Mutates: builder gets the spelling.
func writeCanonicalType(builder *strings.Builder, typed types.Type) {
	switch canonical := types.Unalias(typed).(type) {
	case nil:
		builder.WriteString("<nil>")
	case *types.Basic:
		// byte and rune are their own Basics in go/types, identical to uint8 and int32.
		builder.WriteString(types.Typ[canonical.Kind()].Name())
	case *types.Pointer:
		builder.WriteString("*")
		writeCanonicalType(builder, canonical.Elem())
	case *types.Slice:
		builder.WriteString("[]")
		writeCanonicalType(builder, canonical.Elem())
	case *types.Array:
		builder.WriteString("[" + strconv.FormatInt(canonical.Len(), 10) + "]")
		writeCanonicalType(builder, canonical.Elem())
	case *types.Map:
		builder.WriteString("map[")
		writeCanonicalType(builder, canonical.Key())
		builder.WriteString("]")
		writeCanonicalType(builder, canonical.Elem())
	case *types.Chan:
		builder.WriteString(channelOpenings[canonical.Dir()])
		writeCanonicalType(builder, canonical.Elem())
		builder.WriteString(")")
	case *types.Struct:
		builder.WriteString("struct{")
		for index := range canonical.NumFields() {
			field := canonical.Field(index)
			if index > 0 {
				builder.WriteString("; ")
			}
			if field.Embedded() {
				builder.WriteString("embedded ")
			}
			// An unexported field is identical only to one of the same package, as types.Identical has it.
			if !field.Exported() && field.Pkg() != nil {
				builder.WriteString(field.Pkg().Path() + ".")
			}
			builder.WriteString(field.Name() + " ")
			writeCanonicalType(builder, field.Type())
			if tag := canonical.Tag(index); tag != "" {
				builder.WriteString(" " + strconv.Quote(tag))
			}
		}
		builder.WriteString("}")
	case *types.Signature:
		writeCanonicalSignature(builder, canonical)
	case *types.Interface:
		builder.WriteString("interface{")
		// A complete interface's methods are its whole method set, sorted, embedded interfaces' included.
		for index := range canonical.NumMethods() {
			method := canonical.Method(index)
			if index > 0 {
				builder.WriteString("; ")
			}
			if !method.Exported() && method.Pkg() != nil {
				builder.WriteString(method.Pkg().Path() + ".")
			}
			builder.WriteString(method.Name())
			writeCanonicalSignature(builder, method.Signature())
		}
		for index := range canonical.NumEmbeddeds() {
			// An embedded interface's methods are above; anything else, a union or a type, is a term of the type set.
			embedded := canonical.EmbeddedType(index)
			if _, isInterface := embedded.Underlying().(*types.Interface); isInterface {
				continue
			}
			builder.WriteString("; ")
			writeCanonicalType(builder, embedded)
		}
		builder.WriteString("}")
	case *types.Union:
		for index := range canonical.Len() {
			term := canonical.Term(index)
			if index > 0 {
				builder.WriteString("|")
			}
			if term.Tilde() {
				builder.WriteString("~")
			}
			writeCanonicalType(builder, term.Type())
		}
	case *types.Named:
		name := canonical.Obj()
		if name.Pkg() != nil {
			builder.WriteString(name.Pkg().Path() + ".")
		}
		builder.WriteString(name.Name())
		if arguments := canonical.TypeArgs(); arguments.Len() > 0 {
			builder.WriteString("[")
			for index := range arguments.Len() {
				if index > 0 {
					builder.WriteString(", ")
				}
				writeCanonicalType(builder, arguments.At(index))
			}
			builder.WriteString("]")
		}
	case *types.TypeParam:
		builder.WriteString(canonical.Obj().Name())
	case *types.Tuple:
		writeCanonicalTuple(builder, canonical, false)
	default:
		builder.WriteString(types.TypeString(canonical, func(named *types.Package) string { return named.Path() }))
	}
}

// writeCanonicalSignature writes signature's type parameters' constraints, its parameters' types and its results'
// types, never its receiver or any name, which types.Identical ignores.
//
// Mutates: builder gets the spelling.
func writeCanonicalSignature(builder *strings.Builder, signature *types.Signature) {
	builder.WriteString("func")
	if parameters := signature.TypeParams(); parameters.Len() > 0 {
		builder.WriteString("[")
		for index := range parameters.Len() {
			if index > 0 {
				builder.WriteString(", ")
			}
			writeCanonicalType(builder, parameters.At(index).Constraint())
		}
		builder.WriteString("]")
	}
	writeCanonicalTuple(builder, signature.Params(), signature.Variadic())
	writeCanonicalTuple(builder, signature.Results(), false)
}

// writeCanonicalTuple writes tuple's types in parentheses, the last as `...` and its element when variadic.
//
// Mutates: builder gets the spelling.
func writeCanonicalTuple(builder *strings.Builder, tuple *types.Tuple, variadic bool) {
	builder.WriteString("(")
	for index := range tuple.Len() {
		if index > 0 {
			builder.WriteString(", ")
		}
		typed := tuple.At(index).Type()
		if variadic && index == tuple.Len()-1 {
			// A variadic parameter's type is a slice of its element.
			if slice, isSlice := typed.(*types.Slice); isSlice {
				builder.WriteString("...")
				typed = slice.Elem()
			}
		}
		writeCanonicalType(builder, typed)
	}
	builder.WriteString(")")
}
