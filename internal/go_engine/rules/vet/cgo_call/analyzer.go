// Package cgo_call is vet's cgocall pass, a call to C that hands it a Go value whose type holds Go pointers the cgo
// pointer passing rules almost always forbid: a channel, a map, a function or a slice, passed directly or through a
// pointer, an array or a struct, or as an unsafe.Pointer converted from one.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/cgocall/cgocall.go and
// cgocall_go121.go, whose logic it keeps and whose message it reports word for word, at the same position. As the
// original does, it judges a package only when its types import runtime/cgo, and judges the cgo files as written,
// not the cgo tool's translation of them: it parses each of the package's files again from the file its position
// names, which for a translated file is the file as written, through the translation's `//line` directive, and
// type-checks again those that import "C", against the package already checked (typeCheckCgoSourceFiles). The
// original requires no other analyzer and walks the files it parsed, which no inspector holds, so the port requires
// none either and builds an inspector over those files. It leaves out the original's debugging output, which a
// constant that is always false turns off.
//
// The registry runs the port in the original's place, and the original stays the oracle: the parity tests beside
// this file run both over the same packages. The two differ in one place, held by testdata/src/differences: over a
// cgo file with an import the type checker refused, which it records no package name for, the original's unchecked
// assertion panics and judges nothing of the package, and the port leaves the import out and judges the rest, as a
// pass that runs despite type errors is meant to (importedPackage).
//
// It is not on the engine's syntax dispatcher, and stays a plain analyzer: it type-checks its own copies of the cgo
// files and walks those, not the package's syntax the dispatcher walks.
package cgo_call

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strconv"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/system-inc/cohere/internal/go_engine/type_predicates"
)

// Analyzer runs the port under the original's name, documentation and address, so the rule, its settings and every
// `//cohere:ignore` naming it read the same as before the port took the original's place. It runs despite type
// errors, as the original does, since it type-checks the cgo files again of its own and ignores their errors.
var Analyzer = &analysis.Analyzer{
	Name: "cgocall",
	Doc: "detect some violations of the cgo pointer passing rules\n\n" +
		"Check for invalid cgo pointer passing.\n" +
		"This looks for code that uses cgo to call C code passing values\n" +
		"whose types are almost always invalid according to the cgo pointer\n" +
		"sharing rules.\n" +
		"Specifically, it warns about attempts to pass a Go chan, map, func,\n" +
		"or slice to C, either directly, or via a pointer, array, or struct.",
	URL:              "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/cgocall",
	RunDespiteErrors: true,
	Run:              run,
}

// thisPackagePath is the import path the files type-checked again import the package already checked under. It holds
// middle dots, which no import path can, so it never names a package the files import of their own.
const thisPackagePath = "·this·"

// embeddedPointerMessage is the one message the pass reports.
const embeddedPointerMessage = "possibly passing Go type with embedded pointer to C"

func run(pass *analysis.Pass) (any, error) {
	// A package whose types do not import runtime/cgo does not use cgo. The load marks every package that does never
	// restored from the host's run by the same predicate, which is why the restore can leave this pass's property out
	// of a foreign field's rendering (type_predicates.UsesCgo).
	if !type_predicates.UsesCgo(pass.Pkg) {
		return nil, nil
	}
	cgoFiles, information, checkError := typeCheckCgoSourceFiles(
		pass.Fset, pass.Pkg, pass.Files, pass.TypesInfo, pass.TypesSizes,
	)
	if checkError != nil {
		return nil, checkError
	}
	// All visits each call in preorder, file by file, as the original's ast.Inspect of each file does, and hands it
	// over typed.
	for call := range inspector.All[*ast.CallExpr](inspector.New(cgoFiles)) {
		checkCall(pass, information, call)
	}
	return nil, nil
}

// checkCall reports call, when it calls a C function, at the first argument it hands C that is a Go value of a type
// holding Go pointers, or the address of one, and reports nothing more of it. x/tools' checkCgo, for one call.
func checkCall(pass *analysis.Pass, information *types.Info, call *ast.CallExpr) {
	// Only `C.name(...)` calls C. Any other name than C is not cgo, and C is judged by its name alone, as cgo itself
	// judges it.
	selector, isSelector := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !isSelector {
		return
	}
	if qualifier, isIdentifier := selector.X.(*ast.Ident); !isIdentifier || qualifier.Name != "C" {
		return
	}
	// C.CBytes copies the Go bytes it is handed into C memory, so passing it a slice is always safe.
	if selector.Sel.Name == "CBytes" {
		return
	}
	for _, argument := range call.Args {
		if !typeOkForCgoCall(cgoBaseType(information, argument), map[types.Type]bool{}) {
			pass.Reportf(argument.Pos(), embeddedPointerMessage)
			return
		}
		// The address of a value of a bad type, as written or converted to unsafe.Pointer. Its pointer type was
		// judged above whenever it has one; this judges the value itself, which has a type where the address
		// does not, when the type checker refused to take it (`&values[key]` of a map). The report is at the
		// address, inside any conversion.
		addressed := argument
		if conversion, isCall := argument.(*ast.CallExpr); isCall && len(conversion.Args) == 1 &&
			isUnsafePointer(information, conversion.Fun) {
			addressed = conversion.Args[0]
		}
		if address, isUnary := addressed.(*ast.UnaryExpr); isUnary && address.Op == token.AND {
			if !typeOkForCgoCall(cgoBaseType(information, address.X), map[types.Type]bool{}) {
				pass.Reportf(addressed.Pos(), embeddedPointerMessage)
				return
			}
		}
	}
}

// typeCheckCgoSourceFiles returns the cgo files of a package as written, those that import "C", type-checked against
// the package already checked, with the type information of that check alone. The files are not Go, since they
// name C, so their type information has gaps around each `C.name`, which the analysis bears, since it judges the Go
// values handed to C and never C's own names. x/tools' typeCheckCgoSourceFiles, whose comment tells the whole of
// why at length.
//
// Each file is parsed again from the file its position names, which for one the cgo tool translated is the file as
// written, through its `//line` directive. It is type-checked as a file that dot-imports the package already checked,
// under thisPackagePath, so every name it uses resolves to the object already checked, with its accurate type, and it
// declares nothing to shadow one: its type declarations are dropped, its variables and constants are blanked, each
// function is renamed blank, and each method becomes a function whose first parameter is its receiver. A reference
// to an unexported name is a type error the check ignores, as it ignores every other.
//
// Mutates: fileSet the files parsed again are added to it
func typeCheckCgoSourceFiles(
	fileSet *token.FileSet,
	checked *types.Package,
	files []*ast.File,
	information *types.Info,
	sizes types.Sizes,
) ([]*ast.File, *types.Info, error) {
	var cgoFiles []*ast.File
	importMap := map[string]*types.Package{thisPackagePath: checked}
	for _, compiled := range files {
		// Position honors a `//line` directive, so a file the cgo tool translated names the file as written. The
		// original reads the position of the package clause, not the file's start, since the directive comes after
		// the translation's opening comment and names the clause's line.
		filename := fileSet.Position(compiled.Pos()).Filename
		written, parseError := parser.ParseFile(fileSet, filename, nil, parser.SkipObjectResolution)
		if parseError != nil {
			return nil, nil, fmt.Errorf("can't parse raw cgo file: %w", parseError)
		}
		if !importsC(written) {
			continue
		}
		// The files type-checked again import each path they import as the file compiled imported it.
		for _, specification := range compiled.Imports {
			imported := importedPackage(information, specification)
			// The type checker records no package name for an import it refused: a path that is no import path, a
			// renamed "C", or a package imported as init. The original asserts that it recorded one, unchecked,
			// and panics, judging nothing of the package. The port leaves the import out of importMap, as the type
			// checker left it out of the package, so the files type-checked again fail to import it, unless another
			// import of the same path was accepted, an error ignored with the rest, and the remainder is judged
			// (testdata/src/differences).
			if imported == nil {
				continue
			}
			// Ignored: a path the type checker accepted is a quoted string literal, which unquotes, and the
			// original ignores the error too.
			path, _ := strconv.Unquote(specification.Path.Value)
			importMap[path] = imported
		}
		cgoFiles = append(cgoFiles, declaringNothing(written))
	}
	// A package whose types import runtime/cgo with no file importing "C" imports runtime/cgo of its own, and has
	// nothing to judge.
	if cgoFiles == nil {
		return nil, nil, nil
	}
	configuration := &types.Config{
		GoVersion:   checked.GoVersion(),
		FakeImportC: true,
		Importer: importerFunction(func(path string) (*types.Package, error) {
			return importMap[path], nil
		}),
		Sizes: sizes,
		Error: func(error) {
			// The files are not Go, and import what they no longer use, so their type errors are expected, and
			// ignored.
		},
	}
	// The package's own TypesInfo belongs to the pass, so the check records into information of its own.
	cgoInformation := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
	// Ignored: the check's error is the first of the type errors Error already ignored, and the package it returns
	// declares nothing, since the files declare nothing.
	_, _ = configuration.Check(checked.Path(), fileSet, cgoFiles, cgoInformation)
	return cgoFiles, cgoInformation, nil
}

// importsC reports whether file imports "C", as the original compares the path as written, quotes and all.
func importsC(file *ast.File) bool {
	for _, specification := range file.Imports {
		if specification.Path.Value == `"C"` {
			return true
		}
	}
	return false
}

// declaringNothing is file with a dot import of the package already checked first, and its declarations changed to
// declare nothing that would shadow what that import declares: type declarations dropped, the names of variables and
// constants blanked, and each function and method renamed blank, a method's receiver made its first parameter. Bodies
// and initial values are kept, the uses the analysis judges. The rest of x/tools' typeCheckCgoSourceFiles, for one
// file.
//
// Mutates: file its declarations are replaced, and changed in place
func declaringNothing(file *ast.File) *ast.File {
	declarations := []ast.Decl{&ast.GenDecl{
		Tok: token.IMPORT,
		Specs: []ast.Spec{&ast.ImportSpec{
			Name: &ast.Ident{Name: "."},
			Path: &ast.BasicLit{Kind: token.STRING, Value: strconv.Quote(thisPackagePath)},
		}},
	}}
	for _, declaration := range file.Decls {
		switch declared := declaration.(type) {
		case *ast.GenDecl:
			switch declared.Tok {
			case token.TYPE:
				continue
			case token.VAR, token.CONST:
				for _, specification := range declared.Specs {
					value, isValue := specification.(*ast.ValueSpec)
					if !isValue {
						// Invariant: the parser gives a var or const declaration value specifications alone.
						panic("cgo_call: a var or const declaration holds a specification of another kind")
					}
					for _, name := range value.Names {
						name.Name = "_"
					}
				}
			default:
				// Imports are kept as they are.
			}
		case *ast.FuncDecl:
			declared.Name.Name = "_"
			if declared.Recv != nil {
				declared.Type.Params.List = append(append([]*ast.Field{}, declared.Recv.List...),
					declared.Type.Params.List...)
				declared.Recv = nil
			}
		}
		declarations = append(declarations, declaration)
	}
	file.Decls = declarations
	return file
}

// cgoBaseType is the type of argument, looking through a conversion to unsafe.Pointer, `unsafe.Pointer(value)`, and
// a read of an unsafe.Pointer through a pointer to one converted from an address,
// `*(*unsafe.Pointer)(unsafe.Pointer(&value))`, to the type of value. x/tools' cgoBaseType.
func cgoBaseType(information *types.Info, argument ast.Expr) types.Type {
	switch expression := argument.(type) {
	case *ast.CallExpr:
		if len(expression.Args) == 1 && isUnsafePointer(information, expression.Fun) {
			return cgoBaseType(information, expression.Args[0])
		}
	case *ast.StarExpr:
		if value := unsafePointerReadValue(information, expression); value != nil {
			return cgoBaseType(information, value)
		}
	}
	return information.Types[argument].Type
}

// unsafePointerReadValue is value, when star is `*(*unsafe.Pointer)(unsafe.Pointer(&value))`, or any other spelling
// of a pointer type whose element is unsafe.Pointer for `*unsafe.Pointer`, and nil when it is not. The original's
// chain of checks within cgoBaseType, each stage named as the original names it.
func unsafePointerReadValue(information *types.Info, star *ast.StarExpr) ast.Expr {
	// Here star is *f(v).
	call, isCall := star.X.(*ast.CallExpr)
	if !isCall || len(call.Args) != 1 {
		return nil
	}
	called := information.Types[call.Fun].Type
	if called == nil {
		return nil
	}
	// Here star is *(*p)(v).
	pointer, isPointer := called.Underlying().(*types.Pointer)
	if !isPointer {
		return nil
	}
	// Here star is *(*unsafe.Pointer)(v).
	element, isBasic := pointer.Elem().Underlying().(*types.Basic)
	if !isBasic || element.Kind() != types.UnsafePointer {
		return nil
	}
	// Here star is *(*unsafe.Pointer)(f(v)).
	conversion, isConversion := call.Args[0].(*ast.CallExpr)
	if !isConversion || len(conversion.Args) != 1 {
		return nil
	}
	// Here star is *(*unsafe.Pointer)(unsafe.Pointer(v)).
	if !isUnsafePointer(information, conversion.Fun) {
		return nil
	}
	// Here star is *(*unsafe.Pointer)(unsafe.Pointer(&v)).
	address, isUnary := conversion.Args[0].(*ast.UnaryExpr)
	if !isUnary || address.Op != token.AND {
		return nil
	}
	return address.X
}

// typeOkForCgoCall reports whether a value of typed may be passed to C, which it may not when it holds Go pointers: a
// channel, a map, a function or a slice, or a pointer, an array or a struct holding one. A missing type is passed.
// visited holds the types already judged, each judged once, so a recursive type ends, its second visit passed.
// x/tools' typeOKForCgoCall.
//
// Mutates: visited every type judged is added to it
func typeOkForCgoCall(typed types.Type, visited map[types.Type]bool) bool {
	if typed == nil || visited[typed] {
		return true
	}
	visited[typed] = true
	switch underlying := typed.Underlying().(type) {
	case *types.Chan, *types.Map, *types.Signature, *types.Slice:
		return false
	case *types.Pointer:
		return typeOkForCgoCall(underlying.Elem(), visited)
	case *types.Array:
		return typeOkForCgoCall(underlying.Elem(), visited)
	case *types.Struct:
		for field := range underlying.Fields() {
			if !typeOkForCgoCall(field.Type(), visited) {
				return false
			}
		}
	}
	return true
}

// isUnsafePointer reports whether expression has a type whose underlying type is unsafe.Pointer, as the type
// unsafe.Pointer itself does in a conversion. x/tools' isUnsafePointer.
func isUnsafePointer(information *types.Info, expression ast.Expr) bool {
	typed := information.Types[expression].Type
	return typed != nil && typed.Underlying() == types.Typ[types.UnsafePointer]
}

// importedPackage is the package specification imports, as information records it: under the specification itself
// for an import that names no package, and under its name for one that does. It is nil for an import the type
// checker refused, which it records nothing for. x/tools' imported in cgocall, whose assertion panics there.
func importedPackage(information *types.Info, specification *ast.ImportSpec) *types.Package {
	object, isImplicit := information.Implicits[specification]
	if !isImplicit {
		object = information.Defs[specification.Name]
	}
	packageName, isPackageName := object.(*types.PkgName)
	if !isPackageName {
		return nil
	}
	return packageName.Imported()
}
