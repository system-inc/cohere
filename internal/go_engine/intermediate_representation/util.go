// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation

// This file defines a number of miscellaneous utility functions.

import (
	"go/ast"
	"go/token"
	"go/types"
	"go/version"
	"sync"
	_ "unsafe" // for go:linkname hack

	"honnef.co/go/tools/go/types/typeutil"

	"golang.org/x/exp/typeparams"
)

type unit struct{}

//// AST utilities

// isBlankIdent returns true iff `expr` is an Ident with name "_".
// They have no associated types.Object, and thus no type.
func isBlankIdent(expr ast.Expr) bool {
	id, ok := expr.(*ast.Ident)
	return ok && id.Name == "_"
}

//// Type utilities.  Some of these belong in go/types.

// isString reports whether `resolvedType` is exactly a string type.
// `resolvedType` is assumed to be an Underlying type (not Named or Alias).
func isString(resolvedType types.Type) bool {
	basic, ok := resolvedType.(*types.Basic)
	return ok && basic.Info()&types.IsString != 0
}

// isByteSlice reports whether `resolvedType` is of the form []~bytes.
// `resolvedType` is assumed to be an Underlying type (not Named or Alias).
func isByteSlice(resolvedType types.Type) bool {
	if slice, ok := resolvedType.(*types.Slice); ok {
		basic, _ := slice.Elem().Underlying().(*types.Basic)
		return basic != nil && basic.Kind() == types.Byte
	}
	return false
}

// isRuneSlice reports whether `resolvedType` is of the form []~runes.
// `resolvedType` is assumed to be an Underlying type (not Named or Alias).
func isRuneSlice(resolvedType types.Type) bool {
	if slice, ok := resolvedType.(*types.Slice); ok {
		basic, _ := slice.Elem().Underlying().(*types.Basic)
		return basic != nil && basic.Kind() == types.Rune
	}
	return false
}

// isBasicConvTypes returns true when the type set of a type
// can be one side of a Convert operation. This is when:
// - All are basic, []byte, or []rune.
// - At least 1 is basic.
// - At most 1 is []byte or []rune.
func isBasicConvTypes(typ types.Type) bool {
	basics, cnt := 0, 0
	ok := underIs(typ, func(resolvedType types.Type) bool {
		cnt++
		if isBasic(resolvedType) {
			basics++
			return true
		}
		return isByteSlice(resolvedType) || isRuneSlice(resolvedType)
	})
	return ok && basics >= 1 && cnt-basics <= 1
}

// isPointer reports whether `resolvedType`'s underlying type is a pointer.
func isPointer(resolvedType types.Type) bool {
	return is[*types.Pointer](resolvedType.Underlying())
}

// isPointerCore reports whether `resolvedType`'s core type is a pointer.
//
// (Most pointer manipulation is related to receivers, in which case
// isPointer is appropriate. tecallers can use isPointer(resolvedType).
func isPointerCore(resolvedType types.Type) bool {
	return is[*types.Pointer](typeutil.CoreType(resolvedType))
}

func is[T any](x any) bool {
	_, ok := x.(T)
	return ok
}

// deref returns a pointer's element type; otherwise it returns typ.
func deref(typ types.Type) types.Type {
	orig := typ
	typ = types.Unalias(typ)

	if parameter, ok := typ.(*types.TypeParam); ok {
		if ctyp := typeutil.CoreType(parameter); ctyp != nil {
			// This can happen, for example, with len(T) where T is a
			// type parameter whose core type is a pointer to array.
			typ = ctyp
		}
	}
	if pointer, ok := typ.Underlying().(*types.Pointer); ok {
		return pointer.Elem()
	}
	return orig
}

// recvType returns the receiver type of method obj.
func recvType(object *types.Func) types.Type {
	return object.Type().(*types.Signature).Recv().Type()
}

// fieldOf returns the index'th field of the (core type of) a struct type;
// otherwise returns nil.
func fieldOf(typ types.Type, index int) *types.Var {
	if st, ok := typeutil.CoreType(typ).(*types.Struct); ok {
		if 0 <= index && index < st.NumFields() {
			return st.Field(index)
		}
	}
	return nil
}

// isUntyped reports whether typ is the type of an untyped constant.
func isUntyped(typ types.Type) bool {
	// No Underlying/Unalias: untyped constant types cannot be Named or Alias.
	basic, ok := typ.(*types.Basic)
	return ok && basic.Info()&types.IsUntyped != 0
}

// declaredWithin reports whether an object is declared within a function.
//
// obj must not be a method or a field.
func declaredWithin(object types.Object, function *types.Func) bool {
	if object.Pos() != token.NoPos {
		return function.Scope().Contains(object.Pos()) // trust the positions if they exist.
	}
	if function.Pkg() != object.Pkg() {
		return false // fast path for different packages
	}

	// Traverse Parent() scopes for fn.Scope().
	for scope := object.Parent(); scope != nil; scope = scope.Parent() {
		if scope == function.Scope() {
			return true
		}
	}
	return false
}

// fileGoVersion is the Go version the type checker recorded for file, or the empty string, an unknown
// future version, when it recorded none that is valid. go/ir calls x/tools' internal
// versions.FileVersion here, which a module outside x/tools can't import; this is that function on
// go/version.
func fileGoVersion(info *types.Info, file *ast.File) string {
	if fileVersion := info.FileVersions[file]; version.IsValid(fileVersion) {
		return fileVersion
	}
	return ""
}

// goVersionAtLeast reports whether the file version `value` is release or later, where the empty string is an
// unknown future version and so always later. It is x/tools' internal versions.AtLeast on go/version.
func goVersionAtLeast(value, release string) bool {
	if value == "" {
		return true
	}
	return version.Compare(version.Lang(value), version.Lang(release)) >= 0
}

// newVariable creates a 'var' for use in a types.Tuple.
func newVariable(name string, typ types.Type) *types.Var {
	return types.NewParam(token.NoPos, nil, name, typ)
}

// anonVariable creates an anonymous 'var' for use in a types.Tuple.
func anonVariable(typ types.Type) *types.Var {
	return newVariable("", typ)
}

var lenResults = types.NewTuple(anonVariable(tInt))

// makeLength returns the len builtin specialized to type func(T)int.
func makeLength(T types.Type) *Builtin {
	lenParameters := types.NewTuple(anonVariable(T))
	return &Builtin{
		name: "len",
		sig:  types.NewSignatureType(nil, nil, nil, lenParameters, lenResults, false),
	}
}

func assert(x bool, message string) {
	if !x {
		panic(message)
	}
}

// BlockMap is a mapping from basic blocks (identified by their indices) to values.
type BlockMap[T any] []T

// isBasic reports whether `resolvedType` is a basic type.
func isBasic(resolvedType types.Type) bool {
	_, ok := resolvedType.(*types.Basic)
	return ok
}

// receiverTypeArguments returns the type arguments to a method's receiver.
// Returns an empty list if the receiver does not have type arguments.
func receiverTypeArguments(method *types.Func) []types.Type {
	recv := method.Signature().Recv()
	named := func() (named *types.Named) {
		resolvedType := recv.Type()
		if ptr, ok := types.Unalias(resolvedType).(*types.Pointer); ok {
			resolvedType = ptr.Elem()
		}
		named, _ = types.Unalias(resolvedType).(*types.Named)
		return named
	}()
	if named == nil {
		return nil // recv is anonymous struct/interface
	}
	ts := named.TypeArgs()
	if ts.Len() == 0 {
		return nil
	}
	targs := make([]types.Type, ts.Len())
	for index := 0; index < ts.Len(); index++ {
		targs[index] = ts.At(index)
	}
	return targs
}

// recvAsFirstArgument takes a method signature and returns a function
// signature with receiver as the first parameter.
func recvAsFirstArgument(sig *types.Signature) *types.Signature {
	parameters := make([]*types.Var, 0, 1+sig.Params().Len())
	parameters = append(parameters, sig.Recv())
	for variable := range sig.Params().Variables() {
		parameters = append(parameters, variable)
	}
	return types.NewSignatureType(nil, nil, nil, types.NewTuple(parameters...), sig.Results(), sig.Variadic())
}

// instance returns whether an expression is a simple or qualified identifier
// that is a generic instantiation.
func instance(info *types.Info, expr ast.Expr) bool {
	// Compare the logic here against go/types.instantiatedIdent,
	// which also handles  *IndexExpr and *IndexListExpr.
	var id *ast.Ident
	switch x := expr.(type) {
	case *ast.Ident:
		id = x
	case *ast.SelectorExpr:
		id = x.Sel
	default:
		return false
	}
	_, ok := info.Instances[id]
	return ok
}

// isNonTypeParamInterface reports whether `resolvedType` is an interface type but not a type parameter.
func isNonTypeParamInterface(resolvedType types.Type) bool {
	return !typeparams.IsTypeParam(resolvedType) && types.IsInterface(resolvedType)
}

// instanceArguments returns the Instance[id].TypeArgs as a slice.
func instanceArguments(info *types.Info, id *ast.Ident) []types.Type {
	targList := info.Instances[id].TypeArgs
	if targList == nil {
		return nil
	}

	targs := make([]types.Type, targList.Len())
	for index, length := 0, targList.Len(); index < length; index++ {
		targs[index] = targList.At(index)
	}
	return targs
}

// Mapping of a type T to a canonical instance C s.t. types.Identical(T, C).
// Thread-safe.
type canonizer struct {
	mutex sync.Mutex
	types typeutil.Map[types.Type] // map from type to a canonical instance
	lists typeListMap              // map from a list of types to a canonical instance
}

func newCanonizer() *canonizer {
	canonizer := &canonizer{}
	hasher := typeutil.MakeHasher()
	canonizer.types.SetHasher(hasher)
	canonizer.lists.hasher = hasher
	return canonizer
}

// List returns a canonical representative of a list of types.
// Representative of the empty list is nil.
func (canonizer *canonizer) List(ts []types.Type) *typeList {
	if len(ts) == 0 {
		return nil
	}

	unaliasAll := func(ts []types.Type) []types.Type {
		// Is there some top level alias?
		var found bool
		for _, resolvedType := range ts {
			if _, ok := resolvedType.(*types.Alias); ok {
				found = true
				break
			}
		}
		if !found {
			return ts // no top level alias
		}

		cp := make([]types.Type, len(ts)) // copy with top level aliases removed.
		for index, resolvedType := range ts {
			cp[index] = types.Unalias(resolvedType)
		}
		return cp
	}
	resolvedTypes := unaliasAll(ts)

	canonizer.mutex.Lock()
	defer canonizer.mutex.Unlock()
	return canonizer.lists.rep(resolvedTypes)
}

// Type returns a canonical representative of type T.
// Removes top-level aliases.
//
// For performance, reasons the canonical instance is order-dependent,
// and may contain deeply nested aliases.
func (canonizer *canonizer) Type(T types.Type) types.Type {
	T = types.Unalias(T) // remove the top level alias.

	canonizer.mutex.Lock()
	defer canonizer.mutex.Unlock()

	if resolvedType, _ := canonizer.types.At(T); resolvedType != nil {
		return resolvedType
	}
	canonizer.types.Set(T, T)
	return T
}

// A type for representing a canonized list of types.
type typeList []types.Type

func (list *typeList) identical(ts []types.Type) bool {
	if list == nil {
		return len(ts) == 0
	}
	n := len(*list)
	if len(ts) != n {
		return false
	}
	for index, left := range *list {
		right := ts[index]
		if !types.Identical(left, right) {
			return false
		}
	}
	return true
}

type typeListMap struct {
	hasher  typeutil.Hasher
	buckets map[uint32][]*typeList
}

// rep returns a canonical representative of a slice of types.
func (listMap *typeListMap) rep(ts []types.Type) *typeList {
	if listMap == nil || len(ts) == 0 {
		return nil
	}

	if listMap.buckets == nil {
		listMap.buckets = make(map[uint32][]*typeList)
	}

	h := listMap.hash(ts)
	bucket := listMap.buckets[h]
	for _, list := range bucket {
		if list.identical(ts) {
			return list
		}
	}

	// not present. create a representative.
	cp := make(typeList, len(ts))
	copy(cp, ts)
	rep := &cp

	listMap.buckets[h] = append(bucket, rep)
	return rep
}

func (listMap *typeListMap) hash(ts []types.Type) uint32 {
	if listMap == nil {
		return 0
	}
	// Some smallish prime far away from typeutil.Hash.
	n := len(ts)
	h := uint32(13619) + 2*uint32(n)
	for index := range n {
		h += 3 * listMap.hasher.Hash(ts[index])
	}
	return h
}

// instantiateMethod instantiates `function` with targs and returns a canonical representative for this method.
func (canon *canonizer) instantiateMethod(function *types.Func, targs []types.Type, ctxt *types.Context) *types.Func {
	recv := recvType(function)
	if pointer, ok := types.Unalias(recv).(*types.Pointer); ok {
		recv = pointer.Elem()
	}
	named := types.Unalias(recv).(*types.Named)
	inst, instantiateError := types.Instantiate(ctxt, named.Origin(), targs, false)
	if instantiateError != nil {
		panic(instantiateError)
	}
	rep := canon.Type(inst)
	object, _, _ := types.LookupFieldOrMethod(rep, true, function.Pkg(), function.Name())
	return object.(*types.Func)
}

// Exposed to irutil using the linkname hack.
//
//go:linkname isSyntactic honnef.co/go/tools/go/ir.isSyntactic
func isSyntactic(goEnginePackage *Package) bool { return goEnginePackage.syntax }
