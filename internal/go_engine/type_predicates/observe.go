package type_predicates

import (
	"fmt"
	"go/types"
	"strings"

	"github.com/system-inc/cohere/internal/go_engine/type_predicates/locks"
	"github.com/system-inc/cohere/internal/go_engine/type_predicates/verbs"
)

// observation is one property Observe renders of a field's type: what it is, for the guard's message, and how it is
// rendered under a build's sizes, by the rule's own walk.
type observation struct {
	name   string
	render func(fieldType types.Type, sizes types.Sizes) string
}

// observations are every property any rule reads of another package's unexported field, each by the walk the rule
// itself makes of the field's type, in a fixed order. A rule that reads anything else of such a field needs one more
// here, and the guard test names its file until it has one. vet's cgocall walks a field's type too, through pointers,
// and needs none: it judges only a package whose types import runtime/cgo, and the load marks every such package
// never restored.
var observations = []observation{
	{"size and alignment under the build's sizes (the type checker's unsafe.Sizeof and Alignof, SA1027's offsets)",
		func(fieldType types.Type, sizes types.Sizes) string {
			return fmt.Sprintf("size %d align %d", sizes.Sizeof(fieldType), sizes.Alignof(fieldType))
		}},
	{"comparable (the type checker's ==, a map's key, SA1029's context key)",
		func(fieldType types.Type, _ types.Sizes) string {
			return fmt.Sprintf("comparable %t", types.Comparable(fieldType))
		}},
	{"the lock it holds by value, as the path copylocks prints (locks.Of)",
		func(fieldType types.Type, _ types.Sizes) string {
			path := locks.Of(fieldType)
			if path == nil {
				return "lock none"
			}
			return "lock " + path.String()
		}},
	{"how it answers each set of kinds a printf verb accepts, and whether it has String or Error (verbs.MatchNested)",
		func(fieldType types.Type, _ types.Sizes) string {
			answers := make([]string, 0, len(verbs.AcceptedSets)+1)
			for _, accepted := range verbs.AcceptedSets {
				reason, matched := verbs.MatchNested(accepted, fieldType)
				answer := "fails"
				if matched {
					answer = "matches"
				}
				answers = append(answers, fmt.Sprintf("%d %s %q", accepted, answer, reason))
			}
			answers = append(answers, fmt.Sprintf("stringer %t", verbs.IsConvertibleToString(fieldType)))
			return "printf " + strings.Join(answers, ", ")
		}},
	{"whether binary.Write encodes it, with a bool refused and with one encoded (SA1003's BinaryEncodable)",
		func(fieldType types.Type, _ types.Sizes) string {
			refused := BinaryEncodable(fieldType, func() bool { return false })
			encoded := BinaryEncodable(fieldType, func() bool { return true })
			return fmt.Sprintf("binary %t %t", refused, encoded)
		}},
	{"whether it may hold a float by value (SA4000's MayHoldFloat)",
		func(fieldType types.Type, _ types.Sizes) string {
			return fmt.Sprintf("float %t", MayHoldFloat(fieldType))
		}},
	{"whether it holds only basic values (function_purity's OnlyBasicValues)",
		func(fieldType types.Type, _ types.Sizes) string {
			return fmt.Sprintf("basic %t", OnlyBasicValues(fieldType))
		}},
}

// Properties are what Observe renders of a field's type, each named with the rule whose walk gives it.
func Properties() []string {
	names := make([]string, 0, len(observations))
	for _, observed := range observations {
		names = append(names, observed.name)
	}
	return names
}

// ObservedOnly reports whether an importer learns of field, a field of another package's struct, only what Observe
// renders of its type: it is unexported, so the importer can never name it, and not embedded, so nothing of its type
// is promoted, and its type names no type parameter. A field of a generic struct whose type does is an instance's
// field only once the importer's type arguments are in it, which the properties of the declaration's type do not
// tell, so it keeps its whole type.
func ObservedOnly(field *types.Var) bool {
	return !field.Exported() && !field.Embedded() && !namesTypeParameter(field.Type())
}

// Observe is every property of fieldType, under sizes, the build's, one after another in a fixed order: what a rule's
// walk of a struct can learn of a field of this type, and so all an importer's findings can depend on of another
// package's unexported field. It is rendered in place of the field type's declarations, which no importer can reach
// any other way, so a build whose field type differs only where no walk looks reads the same.
func Observe(fieldType types.Type, sizes types.Sizes) string {
	rendered := make([]string, 0, len(observations))
	for _, observed := range observations {
		rendered = append(rendered, observed.render(fieldType, sizes))
	}
	return strings.Join(rendered, "; ")
}

// namesTypeParameter reports whether walked names a type parameter anywhere, a named type's type arguments included.
// It does not enter a named type's declaration, whose own parameters are bound there.
func namesTypeParameter(walked types.Type) bool {
	switch composite := walked.(type) {
	case *types.TypeParam:
		return true
	case *types.Named:
		for argument := range composite.TypeArgs().Types() {
			if namesTypeParameter(argument) {
				return true
			}
		}
	case *types.Alias:
		for argument := range composite.TypeArgs().Types() {
			if namesTypeParameter(argument) {
				return true
			}
		}
		return namesTypeParameter(types.Unalias(composite))
	case *types.Pointer:
		return namesTypeParameter(composite.Elem())
	case *types.Slice:
		return namesTypeParameter(composite.Elem())
	case *types.Array:
		return namesTypeParameter(composite.Elem())
	case *types.Chan:
		return namesTypeParameter(composite.Elem())
	case *types.Map:
		return namesTypeParameter(composite.Key()) || namesTypeParameter(composite.Elem())
	case *types.Signature:
		return namesTypeParameter(composite.Params()) || namesTypeParameter(composite.Results())
	case *types.Tuple:
		for variable := range composite.Variables() {
			if namesTypeParameter(variable.Type()) {
				return true
			}
		}
	case *types.Struct:
		for field := range composite.Fields() {
			if namesTypeParameter(field.Type()) {
				return true
			}
		}
	case *types.Interface:
		for method := range composite.ExplicitMethods() {
			if namesTypeParameter(method.Type()) {
				return true
			}
		}
		for embedded := range composite.EmbeddedTypes() {
			if namesTypeParameter(embedded) {
				return true
			}
		}
	case *types.Union:
		for index := range composite.Len() {
			if namesTypeParameter(composite.Term(index).Type()) {
				return true
			}
		}
	}
	return false
}
