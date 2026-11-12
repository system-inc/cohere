package check

import (
	"bytes"
	"go/types"
	"slices"
	"strings"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/cache"
)

// methodCandidate is one type name a rule could match for a Method or Interface lookup's name, as either verdict holds
// it, of this run's types or persisted (methodLookupHolds): whether it is an interface or generic, the shape of its
// method of the name, the digest of the lines it is compared across builds by, its method's signature, for a generic
// type its type parameters with their constraints (renderedTypeParameters) and, for an interface, every method of its
// method set, and, for an interface, whether its method set holds the lookup's answer. unconstrained is whether it is
// generic with every type parameter's constraint of all types, which no type argument can fail (#8fbkkp4).
type methodCandidate struct {
	isInterface   bool
	isGeneric     bool
	unconstrained bool
	shape         signatureShape
	lines         []byte
	holdsAnswer   bool
}

// sameLines reports whether two builds' candidates of one type name render alike by their own lines.
func (candidate methodCandidate) sameLines(other methodCandidate) bool {
	return bytes.Equal(candidate.lines, other.lines)
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

// everyConstraintOfAllTypes reports whether every one of parameters has a constraint whose type set is all types, as
// `any` is, which types.Instantiate's validation passes for every type argument in every build: no method, no type
// term, no comparable.
func everyConstraintOfAllTypes(parameters *types.TypeParamList) bool {
	for parameter := range parameters.TypeParams() {
		constraint, isInterface := parameter.Constraint().Underlying().(*types.Interface)
		if !isInterface || !constraint.Empty() {
			return false
		}
	}
	return true
}

// receiverTypeParameters are the type parameters of the type that declares method, nil when it has none: a generic
// concrete type's method has its receiver as an instance of that type over its own receiver type parameters, and an
// interface's method the interface, and either named type gives the declaring type's parameters, which render as the
// receiver's do.
func receiverTypeParameters(method *types.Func) *types.TypeParamList {
	receiver := method.Signature().Recv()
	if receiver == nil {
		return nil
	}
	named, isNamed := types.Unalias(receiver.Type()).(*types.Named)
	if !isNamed {
		return nil
	}
	return named.TypeParams()
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

// ownLinesDigest is the digest of lines, mixed with version, the renderer's, as a persisted read holds a digest
// (persistedDigest), so a candidate of this run and one persisted by a binary of the same renderer compare alike, and
// one of another renderer never does.
func ownLinesDigest(version string, lines ...string) []byte {
	parts := make([][]byte, 0, len(lines))
	for _, line := range lines {
		parts = append(parts, []byte(line))
	}
	return persistedDigest(version, cache.DigestOf(parts...))
}

// answerLinesOf is the digest of the own lines of a lookup's answer under version, the renderer's, as the verdict
// compares it across builds: an interface's every method, or a method's signature, each with the type parameters of
// the generic type that declares it (renderedTypeParameters), and nil for anything else, which no build can answer
// alike. A generic answer's parameters are its lines because a rule that matched it by shape alone, none of its
// parameters bare, matches it by instance in a build where one is, and the signature renders alike in both
// (#8fbkkp4).
func answerLinesOf(answer types.Object, version string) []byte {
	switch declared := answer.(type) {
	case *types.TypeName:
		if interfaceType, isInterface := declared.Type().Underlying().(*types.Interface); isInterface {
			var parameters *types.TypeParamList
			if named, isNamed := types.Unalias(declared.Type()).(*types.Named); isNamed {
				parameters = named.TypeParams()
			}
			return ownLinesDigest(version, "interface", interfaceLine(interfaceType),
				renderedTypeParameters(parameters))
		}
	case *types.Func:
		return ownLinesDigest(version, "method", signatureLine(declared),
			renderedTypeParameters(receiverTypeParameters(declared)))
	}
	return nil
}

// methodCandidateOf is typeName as the rules index it for a lookup of query and name, and false when no rule could
// match it: an interface by the method of its method set, which Interface takes only of an interface a type can
// implement, and for Method a named type by the method it declares, never one an embedded field promotes, which no
// rule indexes. answer is the lookup's answer, nil for none, which an interface's method set may hold, and version the
// renderer's the lines are digested under.
func methodCandidateOf(
	typeName *types.TypeName,
	query string,
	name string,
	answer types.Object,
	version string,
) (methodCandidate, bool) {
	named, isNamed := types.Unalias(typeName.Type()).(*types.Named)
	isGeneric := isNamed && named.TypeParams().Len() > 0
	var parameters *types.TypeParamList
	if isNamed {
		parameters = named.TypeParams()
	}
	unconstrained := isGeneric && everyConstraintOfAllTypes(parameters)
	if interfaceType, isInterface := typeName.Type().Underlying().(*types.Interface); isInterface {
		if query == build_reads.QueryInterface && !interfaceType.IsMethodSet() {
			return methodCandidate{}, false
		}
		for method := range interfaceType.Methods() {
			if method.Name() == name {
				return methodCandidate{isInterface: true, isGeneric: isGeneric, unconstrained: unconstrained,
					shape: shapeOf(method.Signature()), lines: ownLinesDigest(version, signatureLine(method),
						interfaceLine(interfaceType), renderedTypeParameters(parameters)),
					holdsAnswer: answer != nil && slices.ContainsFunc(slices.Collect(interfaceType.Methods()),
						func(held *types.Func) bool { return types.Object(held) == answer })}, true
			}
		}
		return methodCandidate{}, false
	}
	if query == build_reads.QueryInterface || !isNamed {
		return methodCandidate{}, false
	}
	for method := range named.Methods() {
		if method.Name() == name {
			return methodCandidate{isGeneric: isGeneric, unconstrained: unconstrained,
				shape: shapeOf(method.Signature()),
				lines: ownLinesDigest(version, signatureLine(method), renderedTypeParameters(parameters))}, true
		}
	}
	return methodCandidate{}, false
}

// methodCandidatesOf are the candidates among typeNames, a side's type names with a method of the name, by their
// package's path and name, their lines digested under this binary's renderer.
func methodCandidatesOf(
	typeNames []*types.TypeName,
	query string,
	name string,
	answer types.Object,
) map[string]methodCandidate {
	candidates := map[string]methodCandidate{}
	for _, typeName := range typeNames {
		if candidate, isCandidate := methodCandidateOf(typeName, query, name, answer,
			ReadsRendererVersion); isCandidate {
			candidates[objectName(typeName)] = candidate
		}
	}
	return candidates
}

// receiverIsInterface reports whether subject is a method of an interface, whose implementers a rule matches it
// against, rather than a concrete type's.
func receiverIsInterface(subject *types.Func) bool {
	receiver := subject.Signature().Recv()
	return receiver != nil && types.IsInterface(receiver.Type())
}

// receiverEmbedsForeign reports whether subject's receiver embeds a type another package declares, directly or through
// types of its own package that embed one in turn, since the receiver's method set holds every method an embedded
// type promotes, at any depth, and those of another package's type can differ by build.
func receiverEmbedsForeign(subject *types.Func) bool {
	receiver := subject.Signature().Recv()
	if receiver == nil {
		return false
	}
	// visited are the named types of the subject's package already walked, so a type that embeds itself through a
	// pointer, or two that embed each other, end the walk.
	visited := map[*types.Named]bool{}
	var embedsForeign func(embedding types.Type) bool
	embedsForeign = func(embedding types.Type) bool {
		pointee := types.Unalias(embedding)
		if pointer, isPointer := pointee.(*types.Pointer); isPointer {
			pointee = types.Unalias(pointer.Elem())
		}
		if named, isNamed := pointee.(*types.Named); isNamed {
			if named.Obj().Pkg() != nil && named.Obj().Pkg() != subject.Pkg() {
				return true
			}
			if visited[named] {
				return false
			}
			visited[named] = true
		}
		switch underlying := pointee.Underlying().(type) {
		case *types.Struct:
			for field := range underlying.Fields() {
				if field.Embedded() && embedsForeign(field.Type()) {
					return true
				}
			}
		case *types.Interface:
			for embedded := range underlying.EmbeddedTypes() {
				if embedsForeign(embedded) {
					return true
				}
			}
		}
		return false
	}
	return embedsForeign(receiver.Type())
}
