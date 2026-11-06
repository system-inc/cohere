// Source: tsgolint `internal/utilities/type_matches_specifier.go`.
//
// Option specifier matching: how a rule's configuration names a type it wants to allow or forbid, by
// file, by lib, or by package, and how a type in hand is tested against that name.
package type_checking

import (
	"fmt"
	"slices"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
	"github.com/system-inc/cohere/internal/lint/rule"
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
	subjectType *checker.Type,
	names []string,
) bool {
	alias := checker.Type_alias(subjectType)
	var symbol *ast.Symbol
	if alias == nil {
		symbol = checker.Type_symbol(subjectType)
	} else {
		symbol = alias.Symbol()
	}

	if symbol != nil && slices.Contains(names, symbol.Name) {
		return true
	}

	if IsIntrinsicType(subjectType) && slices.Contains(names, subjectType.AsIntrinsicType().IntrinsicName()) {
		return true
	}

	return false
}

func typeDeclaredInFile(
	relativePath string,
	declarationFiles []*ast.SourceFile,
	program rule.Program,
) bool {
	cwd := program.GetCurrentDirectory()
	if relativePath == "" {
		return Some(declarationFiles, func(declarationFile *ast.SourceFile) bool {
			return strings.HasPrefix(declarationFile.FileName().AsString(), cwd)
		})
	}
	absPath := tspath.GetNormalizedAbsolutePath(relativePath, tspath.RootedDirectoryPath(cwd))
	return Some(declarationFiles, func(declarationFile *ast.SourceFile) bool {
		return declarationFile.FileName().AsString() == absPath
	})
}

func typeDeclaredInLib(
	declarationFiles []*ast.SourceFile,
	program rule.Program,
) bool {
	// Assertion: The type is not an error type.

	// Intrinsic type (i.e. string, number, boolean, etc) - Treat it as if it's from lib.
	if len(declarationFiles) == 0 {
		return true
	}
	return Some(declarationFiles, func(declarationFile *ast.SourceFile) bool {
		return rule.IsSourceFileDefaultLibrary(program, declarationFile)
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
	return Some(declarations, func(declaration *ast.Node) bool {
		parentModule := findParentModuleDeclaration(declaration)
		return parentModule != nil && parentModule.Name().Text() == packageName
	})
}

func typeDeclaredInDeclarationFile(
	packageName string,
	declarationFiles []*ast.SourceFile,
	program rule.Program,
) bool {
	// typesPackageName := ""
	//  // Handle scoped packages: if the name starts with @, remove it and replace / with __
	// slashIndex := strings.Index(packageName, "/")
	// if packageName[0] == '@' && slashIndex >= 0 {
	// 	typesPackageName = packageName[1:slashIndex] + "__" + packageName[slashIndex+1:]
	// }

	// tsgo keeps no map from a source file to the package it came from, so a declaration file cannot be
	// traced to its package here, and the answer is no.

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
	program rule.Program,
) bool {
	return typeDeclaredInDeclareModule(packageName, declarations) ||
		typeDeclaredInDeclarationFile(packageName, declarationFiles, program)
}

func typeMatchesSpecifier(
	subjectType *checker.Type,
	specifier TypeOrValueSpecifier,
	program rule.Program,
) bool {
	// A union matches only when every member matches, and an intersection when any member does.
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
	if IsUnionType(subjectType) {
		return Every(subjectType.Types(), func(part *checker.Type) bool {
			return typeMatchesSpecifier(part, specifier, program)
		})
	}

	if wholeTypeMatchesSpecifier(subjectType, specifier, program) {
		return true
	}

	if IsIntersectionType(subjectType) {
		return Some(subjectType.Types(), func(part *checker.Type) bool {
			return typeMatchesSpecifier(part, specifier, program)
		})
	}

	return false
}

// wholeTypeMatchesSpecifier tests one type, without descending into a union or an intersection.
//
// Upstream writes this as an immediately-invoked closure inside `typeMatchesSpecifier`; it is a
// named function here because Go has no such expression and because the caller reads better with
// the three cases separated.
func wholeTypeMatchesSpecifier(
	subjectType *checker.Type,
	specifier TypeOrValueSpecifier,
	program rule.Program,
) bool {
	// The error type has a symbol whose name can incidentally match a specifier, so upstream
	// declines it before any name comparison. `TypeMatchesSomeSpecifier` also skips it, but only
	// for the top-level type, and this recursion can now reach one in a constituent.
	if IsIntrinsicErrorType(subjectType) {
		return false
	}

	if !typeMatchesStringSpecifier(subjectType, specifier.Name) {
		return false
	}

	symbol := checker.Type_symbol(subjectType)
	if symbol == nil {
		alias := checker.Type_alias(subjectType)
		if alias != nil {
			symbol = alias.Symbol()
		}
	}
	var declarations []*ast.Node
	if symbol != nil {
		declarations = symbol.Declarations
	}
	declarationFiles := Map(declarations, func(declaration *ast.Node) *ast.SourceFile {
		return ast.GetSourceFileOfNode(declaration)
	})

	switch specifier.From {
	case TypeOrValueSpecifierFromFile:
		return typeDeclaredInFile(specifier.Path, declarationFiles, program)
	case TypeOrValueSpecifierFromLib:
		return typeDeclaredInLib(declarationFiles, program)
	case TypeOrValueSpecifierFromPackage:
		return typeDeclaredInPackageDeclarationFile(specifier.Package, declarations, declarationFiles, program)
	default:
		// Invariant: every specifier is built by a rule that maps its option's `from` to one of the three
		// constants above, and the zero value is the first of them.
		panic(fmt.Sprintf("unknown type specifier from: %v", specifier.From))
	}
}

func TypeMatchesSomeSpecifier(
	subjectType *checker.Type,
	specifiers []TypeOrValueSpecifier,
	inlineSpecifiers []string,
	program rule.Program,
) bool {
	if Some(specifiers, func(specifier TypeOrValueSpecifier) bool {
		return typeMatchesSpecifier(subjectType, specifier, program)
	}) {
		return true
	}
	return Some(inlineSpecifiers, func(name string) bool {
		return typeMatchesInlineSpecifier(subjectType, name)
	})
}

// typeMatchesInlineSpecifier tests the bare-string specifier form, which matches on name alone.
//
// Upstream expresses this as one branch of `typeMatchesSpecifier`, whose parameter is
// `string | TypeOrValueSpecifier`, so the union and intersection recursions above it apply to the
// string form as well. Our `TypeOrValueSpecifier` is a struct with no string alternative, so the
// inline form is a separate function and the two recursions have to be written here too.
//
// Getting that wrong is invisible from the object form's tests. `only-throw-error` valid case 36
// is `allow: ["Promise"]` over `Promise<T1> | Promise<T2>`, which is the inline form over a union:
// without the `every` recursion here the case reports, and no fixture using the object form can
// see it.
func typeMatchesInlineSpecifier(subjectType *checker.Type, name string) bool {
	if IsUnionType(subjectType) {
		return Every(subjectType.Types(), func(part *checker.Type) bool {
			return typeMatchesInlineSpecifier(part, name)
		})
	}

	if !IsIntrinsicErrorType(subjectType) && typeMatchesStringSpecifier(subjectType, []string{name}) {
		return true
	}

	if IsIntersectionType(subjectType) {
		return Some(subjectType.Types(), func(part *checker.Type) bool {
			return typeMatchesInlineSpecifier(part, name)
		})
	}

	return false
}

// staticNameOfNode is upstream's `getStaticName`: the name a node contributes to a value specifier.
//
// An identifier gives its text; a string literal gives its value, so `a['deprecatedKey']` can be
// allowlisted by the same `name` that would allowlist `a.deprecatedKey`. Anything else has no static
// name and matches nothing.
func staticNameOfNode(node *ast.Node) (string, bool) {
	if node == nil {
		return "", false
	}
	switch node.Kind {
	case ast.KindPrivateIdentifier:
		// A private field's node text carries the `#`, and estree's `node.name` does not, so a
		// specifier written `{ name: 'privateProp' }` would never match `#privateProp`. Upstream's
		// allowlist is written against the estree spelling, so the sigil comes off here.
		return strings.TrimPrefix(node.Text(), "#"), true
	case ast.KindIdentifier, ast.KindJsxNamespacedName:
		return node.Text(), true
	case ast.KindStringLiteral, ast.KindNoSubstitutionTemplateLiteral:
		return node.Text(), true
	}
	return "", false
}

// valueMatchesSpecifier tests one specifier against the value at a node.
//
// This is the value half of the option surface, and it asks a different question from the type half
// above. `typeMatchesSpecifier` asks where a type was declared; this asks whether the name written
// at this node is on the list, and then, for a package specifier, whether the thing that name refers
// to came from that package.
//
// The asymmetry is deliberate upstream and worth keeping: a `file` or `lib` specifier matches on the
// name alone once the name matches, because a value's declaration file is already the file being
// linted or is not interesting. Only `package` looks further, because "the `exists` from `fs`" is a
// different claim from "anything called `exists`".
func valueMatchesSpecifier(
	node *ast.Node,
	specifier TypeOrValueSpecifier,
	program rule.Program,
	subject *checker.Type,
) bool {
	staticName, named := staticNameOfNode(node)
	if !named {
		return false
	}
	if !slices.Contains(specifier.Name, staticName) {
		return false
	}
	if specifier.From != TypeOrValueSpecifierFromPackage {
		return true
	}
	if subject == nil {
		return false
	}
	// Upstream is `type.getSymbol() ?? type.aliasSymbol`, so the type's own symbol wins and the
	// alias is the fallback. The two functions above this one ask alias first, because each needs a
	// different value in the else branch; the order only matters for an aliased type that also
	// carries its own symbol, and this reproduces upstream's.
	//
	// No nil check between the two calls, and that is deliberate rather than an omission: the
	// accessors above guard `alias == nil` explicitly, so a reader comparing this to them will
	// look for the missing guard. `TypeAlias.Symbol()` opens with `if a == nil { return nil }`
	// (types.go:664), and a Go method with a nil-receiver guard is safe to call through a nil
	// pointer, so `Alias()` returning nil for a non-alias type, which is most types, resolves to a
	// nil symbol and falls through to the return below.
	symbol := subject.Symbol()
	if symbol == nil {
		symbol = subject.Alias().Symbol()
	}
	if symbol == nil {
		return false
	}
	declarations := symbol.Declarations
	declarationFiles := make([]*ast.SourceFile, 0, len(declarations))
	for _, declaration := range declarations {
		declarationFiles = append(declarationFiles, ast.GetSourceFileOfNode(declaration))
	}
	return typeDeclaredInPackageDeclarationFile(specifier.Package, declarations, declarationFiles, program)
}

// ValueMatchesSomeSpecifier tests the value at a node against a whole allowlist.
//
// The inline (bare string) form matches on the static name alone, which is upstream's
// `typeof specifier === 'string'` branch.
func ValueMatchesSomeSpecifier(
	node *ast.Node,
	specifiers []TypeOrValueSpecifier,
	inlineSpecifiers []string,
	program rule.Program,
	subject *checker.Type,
) bool {
	staticName, named := staticNameOfNode(node)
	if !named {
		return false
	}
	if slices.Contains(inlineSpecifiers, staticName) {
		return true
	}
	return Some(specifiers, func(specifier TypeOrValueSpecifier) bool {
		return valueMatchesSpecifier(node, specifier, program, subject)
	})
}
