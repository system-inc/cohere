package check

import (
	"go/types"
	"slices"
	"strings"

	"github.com/system-inc/cohere/internal/go_engine/cache"
)

// exportDigest is the digest of a package's API by value: its exportRendering, a line each. It stands for the
// package's export data in outputDigest, which cannot hash those bytes: gcexportdata writes each type once per
// types.Type pointer, so the same types serialize differently by whether the package's dependencies were checked
// from source, where `type CacheHashKey xxh3.Uint128` shares Uint128's struct and every `interface{}` written is a
// type of its own, or read from export data, where each file brings its own struct and every empty interface is one
// (#z14ch7c measured 57 packages of one edit whose export data moved while their API did not).
func exportDigest(checked *types.Package) cache.Key {
	lines := exportRendering(checked)
	parts := make([][]byte, 0, len(lines))
	for _, line := range lines {
		parts = append(parts, []byte(line))
	}
	return cache.DigestOf(parts...)
}

// exportRendering is what an importer's type check can read of a package, written out so that two packages render
// alike only when they declare the same API, and it errs wide, since a rendering that changes when it need not costs
// one package a miss, while one that stays the same when the API moved restores a stale analysis:
//
//   - The package's path, name and Go version.
//   - Every package-level object in its scope, unexported ones too, sorted by name, by kind, name and full type, every
//     named type in it qualified by its package's path. types.TypeString writes a struct's fields in order with their
//     tags and embedded fields as their bare type, an interface's methods and its embedded interfaces apart, a
//     variadic parameter as `...`, a signature's results and its type parameters with their constraints, so the
//     rendering leans on it, and adds what it leaves out: the package of every unexported field and interface method
//     inside an unnamed type, which decides that type's identity and which TypeString writes as the bare name
//     (unexportedOrigins).
//   - A constant's type and exact value.
//   - A defined type's type parameters with their constraints, its underlying type, and every method declared on it,
//     unexported ones too, sorted by name, each with a pointer or value receiver marked, its receiver type and its
//     full signature, then the methods' names in declaration order, since Method(index) hands them to an importer in
//     that order.
//   - An alias's type parameters with their constraints, the type its declaration names, and the type it stands for
//     at the end of any chain of aliases.
//
// What it leaves out on purpose, and why an importer cannot read it:
//
//   - Positions, of every object, field and method. An importer's type check never reads them, and its rules read a
//     position of another package's object only in vet's structtag message, which positionDependent keeps on the
//     first key, or as Related information, which newFinding drops (see outputDigest).
//   - Function bodies, and everything declared in them, local types included. A body is not in export data, so an
//     importer sees what a body does only through the facts its analyzers export, which the fact digests in
//     outputDigest hold. A local type's name is in scope only inside its function, so no package-level declaration's
//     type can name it, and an importer can never reach it through this package's API.
//   - Doc comments and every other comment, which export data does not hold.
//   - The order of package-level declarations, which an importer reads only through Scope, sorted by name.
//   - The declarations of other packages that this package's export data carries, for the types its API reaches. Each
//     is keyed by its own package's output, which the second key reaches through the closure digest of each import
//     (CacheSession.closureOf), as it does this package's imports by path.
//
// What an importer's analysis reads that is neither here nor in a fact is keyed elsewhere: its own files, flags and
// rule set in its first key (cache.Keys), the files a rule reads outside its pass in Caching.ExtraInputs, and the
// output of everything below it in its second key.
func exportRendering(checked *types.Package) []string {
	if checked == nil {
		return []string{"no types"}
	}
	lines := []string{"package " + checked.Path() + " " + checked.Name() + " " + checked.GoVersion()}
	for _, name := range checked.Scope().Names() {
		lines = append(lines, renderedObject(checked.Scope().Lookup(name))...)
	}
	return lines
}

// writtenType is a type as the rendering writes it: by types.TypeString, every named type in it qualified by its
// package's path, then its unexportedOrigins.
func writtenType(written types.Type) string {
	return types.TypeString(written, func(other *types.Package) string { return other.Path() }) +
		unexportedOrigins(written)
}

// renderedObject is the lines of one package-level object as exportRendering writes them.
func renderedObject(object types.Object) []string {
	switch declared := object.(type) {
	case *types.Const:
		return []string{"const " + declared.Name() + " " + writtenType(declared.Type()) + " = " +
			declared.Val().ExactString()}
	case *types.Var:
		return []string{"var " + declared.Name() + " " + writtenType(declared.Type())}
	case *types.Func:
		return []string{"func " + declared.Name() + " " + writtenType(declared.Type())}
	case *types.TypeName:
		return renderedTypeName(declared, nil)
	default:
		return []string{"object " + object.Name() + " " + writtenType(object.Type())}
	}
}

// renderedTypeName is the lines of a type name: an alias, a defined type with its methods, those methodKept keeps, or
// every one when it is nil, or any other type name by its type. A defined type's first line is the one that writes its
// underlying type.
func renderedTypeName(object *types.TypeName, methodKept func(*types.Func) bool) []string {
	switch declared := object.Type().(type) {
	case *types.Alias:
		return []string{"alias " + object.Name() + renderedTypeParameters(declared.TypeParams()) + " = " +
			writtenType(declared.Rhs()) + " is " + writtenType(types.Unalias(declared))}
	case *types.Named:
		lines := []string{"type " + object.Name() + renderedTypeParameters(declared.TypeParams()) + " " +
			writtenType(declared.Underlying())}
		var methods []*types.Func
		for method := range declared.Methods() {
			if methodKept == nil || methodKept(method) {
				methods = append(methods, method)
			}
		}
		inOrder := make([]string, 0, len(methods))
		for _, method := range methods {
			inOrder = append(inOrder, method.Name())
		}
		slices.SortFunc(methods, func(left, right *types.Func) int {
			return strings.Compare(left.Name(), right.Name())
		})
		for _, method := range methods {
			lines = append(lines, renderedMethod(object, method))
		}
		return append(lines, "methods of "+object.Name()+" in order "+strings.Join(inOrder, " "))
	default:
		return []string{"type name " + object.Name() + " " + writtenType(object.Type())}
	}
}

// renderedMethod is the line of one method declared on owner: its name, a pointer or value receiver, its receiver
// type and its full signature.
func renderedMethod(owner *types.TypeName, method *types.Func) string {
	signature, _ := method.Type().(*types.Signature)
	receiver := "value"
	receiverType := "none"
	if signature != nil && signature.Recv() != nil {
		receiverType = writtenType(signature.Recv().Type())
		if _, isPointer := signature.Recv().Type().(*types.Pointer); isPointer {
			receiver = "pointer"
		}
	}
	return "method " + owner.Name() + "." + method.Name() + " " + receiver + " " + receiverType + " " +
		writtenType(method.Type())
}

// renderedTypeParameters is a list of type parameters, each by name and constraint, or nothing for none.
func renderedTypeParameters(parameters *types.TypeParamList) string {
	if parameters.Len() == 0 {
		return ""
	}
	rendered := make([]string, 0, parameters.Len())
	for parameter := range parameters.TypeParams() {
		rendered = append(rendered, parameter.Obj().Name()+" "+writtenType(parameter.Constraint()))
	}
	return "[" + strings.Join(rendered, ", ") + "]"
}

// unexportedOrigins is the package of every unexported field and interface method inside an unnamed type, in the
// order a walk of it meets them, written after it: TypeString writes such a name bare, while the name's package is
// part of the type's identity, so `struct{count int}` from two packages is two types. The walk stops at the
// declaration of a named type, an alias and a type parameter, which TypeString writes by its qualified name, the
// declaration itself rendered with its package. It does not stop at an instance's type arguments: TypeString writes
// `box.Box[struct{count int}]` with the argument bare, and the argument is part of the instance's identity, so an
// instance over first's struct and one over second's are two types, the same for a named type and a generic alias.
// A type parameter's constraint is walked where the parameter is declared, in a signature or a declaration's list.
// It is empty when the type holds no unexported name.
func unexportedOrigins(walked types.Type) string {
	var origins []string
	var walk func(walked types.Type)
	walkSignature := func(signature *types.Signature) {
		for parameter := range signature.TypeParams().TypeParams() {
			walk(parameter.Constraint())
		}
		for variable := range signature.Params().Variables() {
			walk(variable.Type())
		}
		for variable := range signature.Results().Variables() {
			walk(variable.Type())
		}
	}
	walk = func(walked types.Type) {
		switch composite := walked.(type) {
		case *types.Pointer:
			walk(composite.Elem())
		case *types.Slice:
			walk(composite.Elem())
		case *types.Array:
			walk(composite.Elem())
		case *types.Chan:
			walk(composite.Elem())
		case *types.Map:
			walk(composite.Key())
			walk(composite.Elem())
		case *types.Signature:
			walkSignature(composite)
		case *types.Tuple:
			for variable := range composite.Variables() {
				walk(variable.Type())
			}
		case *types.Struct:
			for field := range composite.Fields() {
				if !field.Exported() && field.Pkg() != nil {
					origins = append(origins, "field "+field.Name()+" of "+field.Pkg().Path())
				}
				walk(field.Type())
			}
		case *types.Interface:
			for method := range composite.ExplicitMethods() {
				if !method.Exported() && method.Pkg() != nil {
					origins = append(origins, "method "+method.Name()+" of "+method.Pkg().Path())
				}
				if signature, isSignature := method.Type().(*types.Signature); isSignature {
					walkSignature(signature)
				}
			}
			for embedded := range composite.EmbeddedTypes() {
				walk(embedded)
			}
		case *types.Union:
			for index := range composite.Len() {
				walk(composite.Term(index).Type())
			}
		case *types.Named:
			for argument := range composite.TypeArgs().Types() {
				walk(argument)
			}
		case *types.Alias:
			for argument := range composite.TypeArgs().Types() {
				walk(argument)
			}
		}
	}
	walk(walked)
	if len(origins) == 0 {
		return ""
	}
	return " where " + strings.Join(origins, ", ")
}
