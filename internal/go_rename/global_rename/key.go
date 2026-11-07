// Package global_rename renames declarations whose uses reach past one function: package-level functions,
// types, constants and variables, struct fields, methods, type parameters and labels, across every package
// and test variant of a loaded workspace. It refuses any rename that would change what some identifier or
// selector resolves to, change which interfaces a type satisfies, change a key an encoder writes, or touch a
// file it may not edit. Locals go through the rename package's engine first and are then verified here
// together with everything else, since a renamed local and a renamed package-level name can meet.
package global_rename

import (
	"fmt"
	"go/token"
	"go/types"
	"strings"
)

// Key identifies one declaration across every type-checked copy of its package. go/packages checks a package
// once for itself and again for its tests (`p` and `p [p.test]`), so one declaration is several objects; they
// share the package path (types.Package.Path carries no variant suffix) and the position of the declaring
// identifier. A position is kept as line and column rather than offset, since an object read from export data
// knows only those.
type Key struct {
	PackagePath string
	Filename    string
	Line        int
	Column      int
	// Name stands in for the position of an object that has none: the universe's, and synthesized ones.
	Name string
}

func (key Key) String() string {
	if key.Filename == "" {
		return key.PackagePath + "." + key.Name
	}
	return fmt.Sprintf("%s:%d:%d", key.Filename, key.Line, key.Column)
}

// origin is the generic declaration behind an instantiated field or method, which is what a rename edits.
func origin(object types.Object) types.Object {
	switch typed := object.(type) {
	case *types.Var:
		return typed.Origin()
	case *types.Func:
		return typed.Origin()
	}
	return object
}

func newKeyer(fileSet *token.FileSet) *keyer {
	return &keyer{fileSet: fileSet, keys: map[types.Object]Key{}}
}

// Kind names, as the report groups them.
const (
	KindLocal         = "local"
	KindLabel         = "label"
	KindTypeParameter = "type parameter"
	KindFunc          = "func"
	KindType          = "type"
	KindConst         = "const"
	KindVariable      = "var"
	KindField         = "field"
	KindEmbeddedField = "embedded field"
	KindMethod        = "method"
)

// kindOf names an object's kind. A local variable is told apart by the caller, which knows its package.
func kindOf(object types.Object) string {
	switch typed := object.(type) {
	case *types.Var:
		if typed.IsField() {
			if typed.Embedded() {
				return KindEmbeddedField
			}
			return KindField
		}
		return KindVariable
	case *types.Func:
		if typed.Signature().Recv() != nil {
			return KindMethod
		}
		return KindFunc
	case *types.TypeName:
		if _, isParameter := typed.Type().(*types.TypeParam); isParameter {
			return KindTypeParameter
		}
		return KindType
	case *types.Const:
		return KindConst
	case *types.Label:
		return KindLabel
	case *types.PkgName:
		return "package"
	case *types.Builtin:
		return "builtin"
	case *types.Nil:
		return "nil"
	}
	return strings.TrimPrefix(fmt.Sprintf("%T", object), "*types.")
}
