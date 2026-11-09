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

	"honnef.co/go/tools/go/types/typeutil"

	"golang.org/x/exp/typeparams"
)

type unit struct{}

//// AST utilities

// isBlankIdent returns true iff e is an Ident with name "_".
// They have no associated types.Object, and thus no type.
func isBlankIdent(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == "_"
}

//// Type utilities.  Some of these belong in go/types.

// isString reports whether t is exactly a string type.
// t is assumed to be an Underlying type (not Named or Alias).
func isString(t types.Type) bool {
	basic, ok := t.(*types.Basic)
	return ok && basic.Info()&types.IsString != 0
}

// isByteSlice reports whether t is of the form []~bytes.
// t is assumed to be an Underlying type (not Named or Alias).
func isByteSlice(t types.Type) bool {
	if b, ok := t.(*types.Slice); ok {
		e, _ := b.Elem().Underlying().(*types.Basic)
		return e != nil && e.Kind() == types.Byte
	}
	return false
}

// isRuneSlice reports whether t is of the form []~runes.
// t is assumed to be an Underlying type (not Named or Alias).
func isRuneSlice(t types.Type) bool {
	if b, ok := t.(*types.Slice); ok {
		e, _ := b.Elem().Underlying().(*types.Basic)
		return e != nil && e.Kind() == types.Rune
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
	ok := underIs(typ, func(t types.Type) bool {
		cnt++
		if isBasic(t) {
			basics++
			return true
		}
		return isByteSlice(t) || isRuneSlice(t)
	})
	return ok && basics >= 1 && cnt-basics <= 1
}

// isPointer reports whether t's underlying type is a pointer.
func isPointer(t types.Type) bool {
	return is[*types.Pointer](t.Underlying())
}

// isPointerCore reports whether t's core type is a pointer.
//
// (Most pointer manipulation is related to receivers, in which case
// isPointer is appropriate. tecallers can use isPointer(t).
func isPointerCore(t types.Type) bool {
	return is[*types.Pointer](typeutil.CoreType(t))
}

func is[T any](x any) bool {
	_, ok := x.(T)
	return ok
}

// deref returns a pointer's element type; otherwise it returns typ.
func deref(typ types.Type) types.Type {
	orig := typ
	typ = types.Unalias(typ)

	if t, ok := typ.(*types.TypeParam); ok {
		if ctyp := typeutil.CoreType(t); ctyp != nil {
			// This can happen, for example, with len(T) where T is a
			// type parameter whose core type is a pointer to array.
			typ = ctyp
		}
	}
	if p, ok := typ.Underlying().(*types.Pointer); ok {
		return p.Elem()
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
	b, ok := typ.(*types.Basic)
	return ok && b.Info()&types.IsUntyped != 0
}

// declaredWithin reports whether an object is declared within a function.
//
// obj must not be a method or a field.
func declaredWithin(object types.Object, fn *types.Func) bool {
	if object.Pos() != token.NoPos {
		return fn.Scope().Contains(object.Pos()) // trust the positions if they exist.
	}
	if fn.Pkg() != object.Pkg() {
		return false // fast path for different packages
	}

	// Traverse Parent() scopes for fn.Scope().
	for p := object.Parent(); p != nil; p = p.Parent() {
		if p == fn.Scope() {
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
	if v := info.FileVersions[file]; version.IsValid(v) {
		return v
	}
	return ""
}

// goVersionAtLeast reports whether the file version v is release or later, where the empty string is an
// unknown future version and so always later. It is x/tools' internal versions.AtLeast on go/version.
func goVersionAtLeast(v, release string) bool {
	if v == "" {
		return true
	}
	return version.Compare(version.Lang(v), version.Lang(release)) >= 0
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

// isBasic reports whether t is a basic type.
func isBasic(t types.Type) bool {
	_, ok := t.(*types.Basic)
	return ok
}

// receiverTypeArguments returns the type arguments to a method's receiver.
// Returns an empty list if the receiver does not have type arguments.
func receiverTypeArguments(method *types.Func) []types.Type {
	recv := method.Signature().Recv()
	named := func() (named *types.Named) {
		t := recv.Type()
		if ptr, ok := types.Unalias(t).(*types.Pointer); ok {
			t = ptr.Elem()
		}
		named, _ = types.Unalias(t).(*types.Named)
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
	for i := 0; i < ts.Len(); i++ {
		targs[i] = ts.At(i)
	}
	return targs
}

// recvAsFirstArgument takes a method signature and returns a function
// signature with receiver as the first parameter.
func recvAsFirstArgument(sig *types.Signature) *types.Signature {
	parameters := make([]*types.Var, 0, 1+sig.Params().Len())
	parameters = append(parameters, sig.Recv())
	for v := range sig.Params().Variables() {
		parameters = append(parameters, v)
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

// isNonTypeParamInterface reports whether t is an interface type but not a type parameter.
func isNonTypeParamInterface(t types.Type) bool {
	return !typeparams.IsTypeParam(t) && types.IsInterface(t)
}

// instanceArguments returns the Instance[id].TypeArgs as a slice.
func instanceArguments(info *types.Info, id *ast.Ident) []types.Type {
	targList := info.Instances[id].TypeArgs
	if targList == nil {
		return nil
	}

	targs := make([]types.Type, targList.Len())
	for i, n := 0, targList.Len(); i < n; i++ {
		targs[i] = targList.At(i)
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
	c := &canonizer{}
	h := typeutil.MakeHasher()
	c.types.SetHasher(h)
	c.lists.hasher = h
	return c
}

// List returns a canonical representative of a list of types.
// Representative of the empty list is nil.
func (c *canonizer) List(ts []types.Type) *typeList {
	if len(ts) == 0 {
		return nil
	}

	unaliasAll := func(ts []types.Type) []types.Type {
		// Is there some top level alias?
		var found bool
		for _, t := range ts {
			if _, ok := t.(*types.Alias); ok {
				found = true
				break
			}
		}
		if !found {
			return ts // no top level alias
		}

		cp := make([]types.Type, len(ts)) // copy with top level aliases removed.
		for i, t := range ts {
			cp[i] = types.Unalias(t)
		}
		return cp
	}
	l := unaliasAll(ts)

	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.lists.rep(l)
}

// Type returns a canonical representative of type T.
// Removes top-level aliases.
//
// For performance, reasons the canonical instance is order-dependent,
// and may contain deeply nested aliases.
func (c *canonizer) Type(T types.Type) types.Type {
	T = types.Unalias(T) // remove the top level alias.

	c.mutex.Lock()
	defer c.mutex.Unlock()

	if r, _ := c.types.At(T); r != nil {
		return r
	}
	c.types.Set(T, T)
	return T
}

// A type for representing a canonized list of types.
type typeList []types.Type

func (l *typeList) identical(ts []types.Type) bool {
	if l == nil {
		return len(ts) == 0
	}
	n := len(*l)
	if len(ts) != n {
		return false
	}
	for i, left := range *l {
		right := ts[i]
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
func (m *typeListMap) rep(ts []types.Type) *typeList {
	if m == nil || len(ts) == 0 {
		return nil
	}

	if m.buckets == nil {
		m.buckets = make(map[uint32][]*typeList)
	}

	h := m.hash(ts)
	bucket := m.buckets[h]
	for _, l := range bucket {
		if l.identical(ts) {
			return l
		}
	}

	// not present. create a representative.
	cp := make(typeList, len(ts))
	copy(cp, ts)
	rep := &cp

	m.buckets[h] = append(bucket, rep)
	return rep
}

func (m *typeListMap) hash(ts []types.Type) uint32 {
	if m == nil {
		return 0
	}
	// Some smallish prime far away from typeutil.Hash.
	n := len(ts)
	h := uint32(13619) + 2*uint32(n)
	for i := range n {
		h += 3 * m.hasher.Hash(ts[i])
	}
	return h
}

// instantiateMethod instantiates m with targs and returns a canonical representative for this method.
func (canon *canonizer) instantiateMethod(m *types.Func, targs []types.Type, ctxt *types.Context) *types.Func {
	recv := recvType(m)
	if p, ok := types.Unalias(recv).(*types.Pointer); ok {
		recv = p.Elem()
	}
	named := types.Unalias(recv).(*types.Named)
	inst, instantiateError := types.Instantiate(ctxt, named.Origin(), targs, false)
	if instantiateError != nil {
		panic(instantiateError)
	}
	rep := canon.Type(inst)
	object, _, _ := types.LookupFieldOrMethod(rep, true, m.Pkg(), m.Name())
	return object.(*types.Func)
}
