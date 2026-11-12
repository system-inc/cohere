package check

import (
	"go/types"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
)

// dependenceReach is one package's dependence set as it is gathered: the memo whose renderings it follows, and the
// sizes they are rendered under, the packages reached, by path, those the closures it followed reach, as the memo
// holds them (typeReach), read into paths once it is gathered (gathered), the paths of the Closure lookups that found
// nothing (DependenceSet.UnfoundPaths), the packages whose deprecations it read (DependenceSet.OwnFactPaths), and
// whether a read reads the import closure's membership (DependenceSet.ClosureMembership).
type dependenceReach struct {
	memo       *ReadsBuild
	sizes      types.Sizes
	paths      map[string]bool
	reached    packageBits
	unfound    map[string]bool
	ownFacts   map[string]bool
	membership bool
}

// add adds reached to the set. The universe, of no package, is the same in every build.
func (reach *dependenceReach) add(reached *types.Package) {
	if reached != nil {
		reach.paths[reached.Path()] = true
	}
}

// use adds what the verdict reads of use (useHolds): a whole use by its closure, a member by its own line, and a use
// of a type name by what its kind reads of it, each with the object's counterpart. A kind about a type asked of an
// object that is no type name reads its closure, as useLines does.
func (reach *dependenceReach) use(use build_reads.ObjectUse) {
	switch use.Kind {
	case build_reads.UseWhole:
		reach.closure(use.Object)
		return
	case build_reads.UseMember:
		reach.counterpart(use.Object)
		reach.member(use.Object)
		return
	}
	typeName, isTypeName := use.Object.(*types.TypeName)
	if !isTypeName {
		reach.closure(use.Object)
		return
	}
	reach.typeName(typeName)
	if use.Kind == build_reads.UseMethodSet {
		reach.methodSet(typeName)
	}
}

// closure adds what object's closure digest reads (closureDigest): its counterpart, and every type name the digest's
// walk enters from object's types, with the packages each of those renders reaches.
func (reach *dependenceReach) closure(object types.Object) {
	reach.counterpart(object)
	reach.follow(closureWalkStep(rootTypes(object), reach.sizes, false))
}

// follow adds what every type name step names reaches, and every type name of each type step observes, read whole:
// from each, the type names its node's rendering named (typeNode.edges) and of the types it observed, to the end of the
// closure, each type name's package, as the memo holds it (typeReach). An observed type is read whole because its
// properties, a size, a lock, are computed from all of it: a walk of it with no sizes enters every field, so it names
// every type name those properties read.
func (reach *dependenceReach) follow(step closureStep) {
	roots := withObserved(slices.Clone(step.typeNames), step.observedTypes)
	reach.reached = unitedBits(reach.reached, reach.memo.reaches.reachOf(reach.memo.graph, roots, reach.sizes))
}

// gathered is the set's paths, those the closures followed reach included.
func (reach *dependenceReach) gathered() map[string]bool {
	for _, path := range reach.memo.reaches.pathsOf(reach.reached) {
		reach.paths[path] = true
	}
	return reach.paths
}

// withObserved is pending with the type names every one of observed names, each read whole.
func withObserved(pending []*types.TypeName, observed []types.Type) []*types.TypeName {
	grown := pending
	for _, observedType := range observed {
		grown = append(grown, closureWalkStep([]types.Type{observedType}, nil, false).typeNames...)
	}
	return grown
}

// member adds what object's own line reads as a use of it as a member (useLines): its package, and the types the
// line renders by their properties, the unexported fields of an unnamed struct its type holds, each read whole. Every
// other type the line writes is written by name.
func (reach *dependenceReach) member(object types.Object) {
	reach.add(object.Pkg())
	reach.follow(closureStep{observedTypes: closureWalkStep(rootTypes(object), reach.sizes, false).observedTypes})
}

// typeName adds what a use of typeName's name or underlying type reads (typeNameLine): its counterpart, and for an
// alias, every alias of its chain and the defined type it ends at, which give its kind and its underlying type. A
// defined type's underlying type is written in its own package's line, whatever package it was declared from.
func (reach *dependenceReach) typeName(typeName *types.TypeName) {
	reach.counterpart(typeName)
	current := typeName.Type()
	for {
		alias, isAlias := current.(*types.Alias)
		if !isAlias {
			break
		}
		reach.add(alias.Obj().Pkg())
		current = alias.Rhs()
	}
	if named, isNamed := current.(*types.Named); isNamed {
		reach.add(named.Origin().Obj().Pkg())
	}
}

// methodSet adds what a use of typeName's method set reads past its name (methodSetLines): every package of a type
// embedded in it at any depth, whose fields and methods decide which methods are promoted. Every method of the set is
// declared by the type or by one of those, so their packages hold each method's package too.
func (reach *dependenceReach) methodSet(typeName *types.TypeName) {
	reach.embedded(typeName.Type())
}

// selection adds what the verdict reads of selection (selectionHolds): the receiver's counterpart, every package of a
// type embedded in the receiver at any depth, which the build's lookup of the name from it reads for a shallower
// member that shadows it and a twin at its depth that makes it ambiguous, and the selected member's own line.
func (reach *dependenceReach) selection(selection build_reads.Selection) {
	reach.counterpart(selection.Receiver)
	reach.embedded(selection.Receiver.Type())
	reach.member(selection.Selected)
}

// lookup adds what the verdict reads of lookup (lookupHolds and deprecationHolds), and reports whether it could: every
// Scope, Closure and PackageName lookup reads the import closure's membership, since every import must be in the build
// (importRoots); a Scope lookup reads its path's package, which the build finds by its path, and the answer by its
// closure (sameObject); a Closure lookup asks the closure, never the path, so it reads the found answer's closure, or
// its path as an unfound one (DependenceSet.UnfoundPaths); a PackageName lookup reads the membership and its
// packages' names alone; a Deprecated lookup reads its path's own facts (DependenceSet.OwnFactPaths), and the
// subject's counterpart. An Interface or Method lookup reads every type name of the closure with a method of
// its name, so its set is the whole closure, as is a lookup of a kind the set does not know.
func (reach *dependenceReach) lookup(lookup build_reads.Lookup) bool {
	switch lookup.Query {
	case build_reads.QueryDeprecated:
		reach.ownFacts[lookup.Path] = true
		if lookup.Subject != nil {
			reach.counterpart(lookup.Subject)
		}
		return true
	case build_reads.QueryScope:
		reach.paths[lookup.Path] = true
		reach.membership = true
		if lookup.Answer != nil {
			reach.closure(lookup.Answer)
		}
		return true
	case build_reads.QueryClosure:
		reach.membership = true
		if lookup.Answer == nil {
			reach.unfound[lookup.Path] = true
			return true
		}
		reach.closure(lookup.Answer)
		return true
	case build_reads.QueryPackageName:
		reach.membership = true
		return true
	default:
		return false
	}
}

// counterpart adds what finding object's counterpart in a build reads (verdict.counterpart): its package, and the
// packages of every type each interface its objectpath indexes a method of embeds at any depth. An interface's method
// is found by its index in the interface's whole method set, sorted, embedded methods counted, so another package
// adding a method to an interface it embeds moves the index. An object of the universe or unsafe, the same in every
// build, and one with no objectpath, which the verdict declines in every build, read nothing more.
func (reach *dependenceReach) counterpart(object types.Object) {
	if object.Pkg() == nil || object.Pkg() == types.Unsafe {
		return
	}
	reach.add(object.Pkg())
	if pathIndexesNoInterface(object) {
		return
	}
	path, hasPath := reach.memo.pathOf(object)
	if !hasPath {
		return
	}
	_, indexedInterfaces := objectPathInterfaces(object.Pkg(), path)
	for _, indexed := range indexedInterfaces {
		reach.embedded(indexed)
	}
}

// pathIndexesNoInterface reports whether object's objectpath surely passes through no interface's method set, told
// without encoding it, which a set of every package of a large workspace would pay for every object it reads: a
// package-level object's path is its name alone, and a method a defined type that is no interface declares is found
// among that type's own methods, as objectpath encodes both (its For, steps 2 and the concrete method's fast path).
// Any other object's path is decoded (objectPathInterfaces).
func pathIndexesNoInterface(object types.Object) bool {
	if object.Pkg().Scope().Lookup(object.Name()) == object {
		return true
	}
	method, isMethod := object.(*types.Func)
	if !isMethod || method.Signature().Recv() == nil || method.Origin() != method {
		return false
	}
	receiver := types.Unalias(method.Signature().Recv().Type())
	if pointer, isPointer := receiver.(*types.Pointer); isPointer {
		receiver = types.Unalias(pointer.Elem())
	}
	named, isNamed := receiver.(*types.Named)
	return isNamed && !types.IsInterface(named)
}

// embedded adds the package of every named type walked is or embeds, at any depth, through pointers, aliases, the
// embedded fields of structs and the embedded types of interfaces, and of every alias on the way: what a lookup of a
// field or method from it (types.LookupFieldOrMethod) and its method set (types.NewMethodSet) can read, since a field
// or method at any depth can shadow, promote or make ambiguous another of its name. Both are go/types' own walks,
// which tell no one what they visit, so this is the walk of the same embedded types.
func (reach *dependenceReach) embedded(walked types.Type) {
	seen := map[types.Type]bool{}
	var walk func(walked types.Type)
	walk = func(walked types.Type) {
		if seen[walked] {
			return
		}
		seen[walked] = true
		switch current := walked.(type) {
		case *types.Alias:
			reach.add(current.Obj().Pkg())
			walk(current.Rhs())
		case *types.Pointer:
			walk(current.Elem())
		case *types.Named:
			reach.add(current.Origin().Obj().Pkg())
			walk(current.Underlying())
		case *types.Struct:
			for field := range current.Fields() {
				if field.Embedded() {
					walk(field.Type())
				}
			}
		case *types.Interface:
			for embedded := range current.EmbeddedTypes() {
				walk(embedded)
			}
		case *types.Union:
			for index := range current.Len() {
				walk(current.Term(index).Type())
			}
		}
	}
	walk(walked)
}

// objectPathInterfaces is the object path, an objectpath of an object of owner, names, and the interfaces it indexes a
// method of on the way, decoded as objectpath.Object decodes it, which tells no one the types it passes through, so
// each step here mirrors its step: an object's type ('.'), a type's element, key, parameters, results, underlying
// type, type parameter, receiver type parameter, constraint or right-hand side (E K P R U T r C a), each type unaliased
// first, and an object of a tuple, a struct, a method set or a type (A F M O). It mirrors Object in
// go/types/objectpath/objectpath.go of golang.org/x/tools v0.50.0; TestTheObjectPathMirrorLandsWhereObjectpathDoes is
// the tripwire for a change there, landing on objectpath's own object for every path the fixtures read. A path it
// cannot follow gives no object and the interfaces met so far, which cannot happen to a path the host's own encoder
// wrote.
func objectPathInterfaces(owner *types.Package, path objectpath.Path) (types.Object, []*types.Interface) {
	name, suffix, hasSuffix := strings.Cut(string(path), ".")
	object := owner.Scope().Lookup(name)
	if object == nil || !hasSuffix {
		return object, nil
	}
	var interfaces []*types.Interface
	current := object.Type()
	object = nil
	for suffix != "" {
		code := suffix[0]
		suffix = suffix[1:]
		index := 0
		if strings.IndexByte("AFMTr", code) >= 0 {
			rest := strings.TrimLeft(suffix, "0123456789")
			parsed, parseError := strconv.Atoi(suffix[:len(suffix)-len(rest)])
			if parseError != nil {
				return nil, interfaces
			}
			index, suffix = parsed, rest
		}
		if code == '.' {
			if object == nil {
				return nil, interfaces
			}
			current, object = object.Type(), nil
			continue
		}
		if current == nil {
			return nil, interfaces
		}
		var indexed *types.Interface
		current, object, indexed = objectPathStep(types.Unalias(current), code, index)
		if indexed != nil {
			interfaces = append(interfaces, indexed)
		}
		if current == nil && object == nil {
			return nil, interfaces
		}
	}
	return object, interfaces
}

// objectPathStep is one step of an objectpath from walked, unaliased, by the step's code and index: the type or the
// object it reaches, and the interface it found a method of by its index, or none of them when the step does not
// apply to walked.
func objectPathStep(walked types.Type, code byte, index int) (types.Type, types.Object, *types.Interface) {
	switch code {
	case 'E':
		// A pointer, slice, array, channel or map, as objectpath asks.
		if element, hasElement := walked.(interface{ Elem() types.Type }); hasElement {
			return element.Elem(), nil, nil
		}
	case 'K':
		if mapped, isMap := walked.(*types.Map); isMap {
			return mapped.Key(), nil, nil
		}
	case 'P':
		if signature, isSignature := walked.(*types.Signature); isSignature {
			return signature.Params(), nil, nil
		}
	case 'R':
		if signature, isSignature := walked.(*types.Signature); isSignature {
			return signature.Results(), nil, nil
		}
	case 'U':
		if named, isNamed := walked.(*types.Named); isNamed {
			return named.Underlying(), nil, nil
		}
	case 'a':
		// objectpath unaliases a type before each step, so a right-hand side's step leaves it as it is.
		return walked, nil, nil
	case 'T':
		if parameterized, hasParameters := walked.(interface{ TypeParams() *types.TypeParamList }); hasParameters &&
			index < parameterized.TypeParams().Len() {
			return parameterized.TypeParams().At(index), nil, nil
		}
	case 'r':
		if signature, isSignature := walked.(*types.Signature); isSignature &&
			index < signature.RecvTypeParams().Len() {
			return signature.RecvTypeParams().At(index), nil, nil
		}
	case 'C':
		if parameter, isParameter := walked.(*types.TypeParam); isParameter {
			return parameter.Constraint(), nil, nil
		}
	case 'A':
		if tuple, isTuple := walked.(*types.Tuple); isTuple && index < tuple.Len() {
			return nil, tuple.At(index), nil
		}
	case 'F':
		if structure, isStruct := walked.(*types.Struct); isStruct && index < structure.NumFields() {
			return nil, structure.Field(index), nil
		}
	case 'M':
		switch methods := walked.(type) {
		case *types.Interface:
			if index < methods.NumMethods() {
				return nil, methods.Method(index), methods
			}
		case *types.Named:
			if index < methods.NumMethods() {
				return nil, methods.Method(index), nil
			}
		}
	case 'O':
		if owned, hasOwner := walked.(interface{ Obj() *types.TypeName }); hasOwner {
			return nil, owned.Obj(), nil
		}
	}
	return nil, nil, nil
}
