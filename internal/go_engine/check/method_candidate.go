package check

import (
	"go/types"
	"slices"
	"strings"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
)

// methodCandidate is one type name a rule could match for a lookup's name: its method of that name, its interface
// type when it is one, nil otherwise, whether it is generic, and the lines it is compared across builds by, its
// method's signature and, for an interface, every method of its method set.
type methodCandidate struct {
	method        *types.Func
	interfaceType *types.Interface
	isGeneric     bool
	signatureLine string
	interfaceLine string
}

// sameLines reports whether two builds' candidates of one type name render alike by their own lines.
func (candidate methodCandidate) sameLines(other methodCandidate) bool {
	return candidate.signatureLine == other.signatureLine && candidate.interfaceLine == other.interfaceLine
}

// signatureShape is what every rule's match of a method needs at least: as many parameters and results, variadic the
// same.
type signatureShape struct {
	parameters int
	results    int
	variadic   bool
}

// shapeOf is signature's shape.
func shapeOf(signature *types.Signature) signatureShape {
	return signatureShape{parameters: signature.Params().Len(), results: signature.Results().Len(),
		variadic: signature.Variadic()}
}

// pathQualifier names every package by its path, so one signature rendered in two builds compares as text.
func pathQualifier(qualified *types.Package) string {
	return qualified.Path()
}

// signatureLine is method's signature without its receiver, every named type by its package's path and name.
func signatureLine(method *types.Func) string {
	return types.TypeString(method.Type(), pathQualifier)
}

// interfaceLine is every method of an interface's method set, embedded ones included, by name and signature, sorted.
func interfaceLine(interfaceType *types.Interface) string {
	lines := make([]string, 0, interfaceType.NumMethods())
	for method := range interfaceType.Methods() {
		lines = append(lines, method.Name()+" "+signatureLine(method))
	}
	slices.Sort(lines)
	return strings.Join(lines, "\n")
}

// methodCandidateOf is typeName as the rules index it for a lookup of query and name, and false when no rule could
// match it: an interface by the method of its method set, which Interface takes only of an interface a type can
// implement, and for Method a named type by the method it declares, never one an embedded field promotes, which no
// rule indexes.
func methodCandidateOf(typeName *types.TypeName, query string, name string) (methodCandidate, bool) {
	named, isNamed := types.Unalias(typeName.Type()).(*types.Named)
	isGeneric := isNamed && named.TypeParams().Len() > 0
	if interfaceType, isInterface := typeName.Type().Underlying().(*types.Interface); isInterface {
		if query == build_reads.QueryInterface && !interfaceType.IsMethodSet() {
			return methodCandidate{}, false
		}
		for method := range interfaceType.Methods() {
			if method.Name() == name {
				return methodCandidate{method: method, interfaceType: interfaceType, isGeneric: isGeneric,
					signatureLine: signatureLine(method), interfaceLine: interfaceLine(interfaceType)}, true
			}
		}
		return methodCandidate{}, false
	}
	if query == build_reads.QueryInterface || !isNamed {
		return methodCandidate{}, false
	}
	for method := range named.Methods() {
		if method.Name() == name {
			return methodCandidate{method: method, isGeneric: isGeneric, signatureLine: signatureLine(method)}, true
		}
	}
	return methodCandidate{}, false
}

// methodCandidatesOf are the candidates among typeNames, a side's type names with a method of the name, by their
// package's path and name.
func methodCandidatesOf(typeNames []*types.TypeName, query string, name string) map[string]methodCandidate {
	candidates := map[string]methodCandidate{}
	for _, typeName := range typeNames {
		if candidate, isCandidate := methodCandidateOf(typeName, query, name); isCandidate {
			candidates[objectName(typeName)] = candidate
		}
	}
	return candidates
}

// receiverEmbedsForeign reports whether subject's receiver embeds a type another package declares, whose promoted
// methods are in the receiver's method set.
func receiverEmbedsForeign(subject *types.Func) bool {
	receiver := subject.Signature().Recv()
	if receiver == nil {
		return false
	}
	foreign := func(embedded types.Type) bool {
		pointee := types.Unalias(embedded)
		if pointer, isPointer := pointee.(*types.Pointer); isPointer {
			pointee = types.Unalias(pointer.Elem())
		}
		named, isNamed := pointee.(*types.Named)
		return isNamed && named.Obj().Pkg() != nil && named.Obj().Pkg() != subject.Pkg()
	}
	receiverType := types.Unalias(receiver.Type())
	if pointer, isPointer := receiverType.(*types.Pointer); isPointer {
		receiverType = types.Unalias(pointer.Elem())
	}
	switch underlying := receiverType.Underlying().(type) {
	case *types.Struct:
		for field := range underlying.Fields() {
			if field.Embedded() && foreign(field.Type()) {
				return true
			}
		}
	case *types.Interface:
		for embedded := range underlying.EmbeddedTypes() {
			if foreign(embedded) {
				return true
			}
		}
	}
	return false
}
