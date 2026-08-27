// Source: tsgolint `internal/utils/type_matches_specifier.go`.
//
// Option specifier matching: how a rule's configuration names a type it wants to allow or forbid, by
// file, by lib, or by package, and how a type in hand is tested against that name.
package typecheck

import (
	"fmt"
	"slices"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/microsoft/TypeScript/tsc/shim/compiler"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
)

type TypeOrValueSpecifierFrom uint8

const (
	TypeOrValueSpecifierFromFile TypeOrValueSpecifierFrom = iota
	TypeOrValueSpecifierFromLib
	TypeOrValueSpecifierFromPackage
)

type TypeOrValueSpecifier struct {
	From TypeOrValueSpecifierFrom
	Name []string
	// Can be used when From == TypeOrValueSpecifierFromFile
	Path string
	// Can be used when From == TypeOrValueSpecifierFromPackage
	Package string
}

func typeMatchesStringSpecifier(
	t *checker.Type,
	names []string,
) bool {
	alias := checker.Type_alias(t)
	var symbol *ast.Symbol
	if alias == nil {
		symbol = checker.Type_symbol(t)
	} else {
		symbol = alias.Symbol()
	}

	if symbol != nil && slices.Contains(names, symbol.Name) {
		return true
	}

	if IsIntrinsicType(t) && slices.Contains(names, t.AsIntrinsicType().IntrinsicName()) {
		return true
	}

	return false
}

func typeDeclaredInFile(
	relativePath string,
	declarationFiles []*ast.SourceFile,
	program *compiler.Program,
) bool {
	cwd := program.Host().GetCurrentDirectory()
	if relativePath == "" {
		return Some(declarationFiles, func(f *ast.SourceFile) bool {
			return strings.HasPrefix(f.FileName(), cwd)
		})
	}
	absPath := tspath.GetNormalizedAbsolutePath(relativePath, cwd)
	return Some(declarationFiles, func(f *ast.SourceFile) bool {
		return f.FileName() == absPath
	})
}

func typeDeclaredInLib(
	declarationFiles []*ast.SourceFile,
	program *compiler.Program,
) bool {
	// Assertion: The type is not an error type.

	// Intrinsic type (i.e. string, number, boolean, etc) - Treat it as if it's from lib.
	if len(declarationFiles) == 0 {
		return true
	}
	return Some(declarationFiles, func(d *ast.SourceFile) bool {
		return IsSourceFileDefaultLibrary(program, d)
	})
}

func findParentModuleDeclaration(
	node *ast.Node,
) *ast.ModuleDeclaration {
	switch node.Kind {
	case ast.KindModuleDeclaration:
		decl := node.AsModuleDeclaration()
		if ast.IsStringLiteral(decl.Name()) {
			return decl
		}
		return nil
	case ast.KindSourceFile:
		return nil
	default:
		return findParentModuleDeclaration(node.Parent)
	}
}

func typeDeclaredInDeclareModule(
	packageName string,
	declarations []*ast.Node,
) bool {
	return Some(declarations, func(d *ast.Node) bool {
		parentModule := findParentModuleDeclaration(d)
		return parentModule != nil && parentModule.Name().Text() == packageName
	})
}

func typeDeclaredInDeclarationFile(
	packageName string,
	declarationFiles []*ast.SourceFile,
	program *compiler.Program,
) bool {
	// typesPackageName := ""
	//  // Handle scoped packages: if the name starts with @, remove it and replace / with __
	// slashIndex := strings.Index(packageName, "/")
	// if packageName[0] == '@' && slashIndex >= 0 {
	// 	typesPackageName = packageName[1:slashIndex] + "__" + packageName[slashIndex+1:]
	// }

	// TODO(port): there is no sourceFileToPackageName anymore
	// it looks like there is no other way to know sourceFile2PackageName,
	// other than set package name for ast.SourceFile in resolver

	return false

	// const matcher = new RegExp(`${packageName}|${typesPackageName}`);
	// return declarationFiles.some(declaration => {
	//   const packageIdName = program.sourceFileToPackageName.get(declaration.path);
	//   return (
	//     packageIdName != null &&
	//     matcher.test(packageIdName) &&
	//     program.isSourceFileFromExternalLibrary(declaration)
	//   );
	// });
}

func typeDeclaredInPackageDeclarationFile(
	packageName string,
	declarations []*ast.Node,
	declarationFiles []*ast.SourceFile,
	program *compiler.Program,
) bool {
	return typeDeclaredInDeclareModule(packageName, declarations) ||
		typeDeclaredInDeclarationFile(packageName, declarationFiles, program)
}

func typeMatchesSpecifier(
	t *checker.Type,
	specifier TypeOrValueSpecifier,
	program *compiler.Program,
) bool {
	// A UNION matches only when EVERY member matches, and an INTERSECTION when ANY member does.
	//
	// Both recursions are upstream's (`TypeOrValueSpecifier.ts`) and neither was here: this file
	// was ported from a tsgolint revision that predates them, so a specifier tested against a
	// composite type saw only the composite's own symbol, which is nil, and answered false.
	//
	// The union arm is the one with a corpus behind it. `only-throw-error` valid case 36 allows
	// `"Promise"` and throws `Promise<T1> | Promise<T2>`, which upstream accepts; its invalid case
	// 36 is the same union with `| void` added, which upstream reports. That pair is exactly the
	// `every` semantics and nothing weaker reproduces both rows. Measured on the installed rule:
	//
	//	Promise<T1> | Promise<T2>          allow: ["Promise"]   SILENT
	//	Promise<T1> | Promise<T2> | void   allow: ["Promise"]   REPORTS
	//	Promise<number> | string           allow: ["Promise"]   REPORTS
	//	Promise<number> & { a: 1 }         allow: ["Promise"]   SILENT
	//
	// The last row is the intersection arm, and it is `some` rather than `every`, which is the
	// asymmetry a reader would most likely collapse.
	if IsUnionType(t) {
		return Every(t.Types(), func(part *checker.Type) bool {
			return typeMatchesSpecifier(part, specifier, program)
		})
	}

	if wholeTypeMatchesSpecifier(t, specifier, program) {
		return true
	}

	if IsIntersectionType(t) {
		return Some(t.Types(), func(part *checker.Type) bool {
			return typeMatchesSpecifier(part, specifier, program)
		})
	}

	return false
}

// wholeTypeMatchesSpecifier tests ONE type, without descending into a union or an intersection.
//
// Upstream writes this as an immediately-invoked closure inside `typeMatchesSpecifier`; it is a
// named function here because Go has no such expression and because the caller reads better with
// the three cases separated.
func wholeTypeMatchesSpecifier(
	t *checker.Type,
	specifier TypeOrValueSpecifier,
	program *compiler.Program,
) bool {
	// The error type has a symbol whose name can incidentally match a specifier, so upstream
	// declines it before any name comparison. `TypeMatchesSomeSpecifier` also skips it, but only
	// for the top-level type, and this recursion can now reach one in a constituent.
	if IsIntrinsicErrorType(t) {
		return false
	}

	if !typeMatchesStringSpecifier(t, specifier.Name) {
		return false
	}

	symbol := checker.Type_symbol(t)
	if symbol == nil {
		alias := checker.Type_alias(t)
		if alias != nil {
			symbol = alias.Symbol()
		}
	}
	var declarations []*ast.Node
	if symbol != nil {
		declarations = symbol.Declarations
	}
	declarationFiles := Map(declarations, func(d *ast.Node) *ast.SourceFile {
		return ast.GetSourceFileOfNode(d)
	})

	switch specifier.From {
	case TypeOrValueSpecifierFromFile:
		return typeDeclaredInFile(specifier.Path, declarationFiles, program)
	case TypeOrValueSpecifierFromLib:
		return typeDeclaredInLib(declarationFiles, program)
	case TypeOrValueSpecifierFromPackage:
		return typeDeclaredInPackageDeclarationFile(specifier.Package, declarations, declarationFiles, program)
	default:
		panic(fmt.Sprintf("unknown type specifier from: %v", specifier.From))
	}
}

func TypeMatchesSomeSpecifier(
	t *checker.Type,
	specifiers []TypeOrValueSpecifier,
	inlineSpecifiers []string,
	program *compiler.Program,
) bool {
	if Some(specifiers, func(s TypeOrValueSpecifier) bool {
		return typeMatchesSpecifier(t, s, program)
	}) {
		return true
	}
	return Some(inlineSpecifiers, func(name string) bool {
		return typeMatchesInlineSpecifier(t, name)
	})
}

// typeMatchesInlineSpecifier tests the bare-string specifier form, which matches on NAME alone.
//
// Upstream expresses this as one branch of `typeMatchesSpecifier`, whose parameter is
// `string | TypeOrValueSpecifier`, so the union and intersection recursions above it apply to the
// string form as well. Our `TypeOrValueSpecifier` is a struct with no string alternative, so the
// inline form is a separate function and the two recursions have to be written here too.
//
// Getting that wrong is invisible from the object form's tests. `only-throw-error` valid case 36
// is `allow: ["Promise"]` over `Promise<T1> | Promise<T2>`, which is the INLINE form over a union:
// without the `every` recursion here the case reports, and no fixture using the object form can
// see it.
func typeMatchesInlineSpecifier(t *checker.Type, name string) bool {
	if IsUnionType(t) {
		return Every(t.Types(), func(part *checker.Type) bool {
			return typeMatchesInlineSpecifier(part, name)
		})
	}

	if !IsIntrinsicErrorType(t) && typeMatchesStringSpecifier(t, []string{name}) {
		return true
	}

	if IsIntersectionType(t) {
		return Some(t.Types(), func(part *checker.Type) bool {
			return typeMatchesInlineSpecifier(part, name)
		})
	}

	return false
}
