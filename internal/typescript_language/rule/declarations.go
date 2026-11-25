package rule

import (
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
)

// DeclarationsIn returns the declarations of symbol that sit in sourceFile, in the checker's order.
//
// A symbol's declarations can live in any file the checker reaches, and a rule keyed on the shapes of
// its imports must not read into another file's bodies (TypeReach). Most rules that ask for a
// declaration only want the one their own file holds: ESLint's core rules see a single file, and its
// scope manager knows no declaration outside it. Reading through this says so in a way the type-reach
// guard can check, since it trusts this function's result to be the rule's own file (#9bjjk4a).
//
// The common case, every declaration in the file, returns the checker's own slice without copying.
func DeclarationsIn(sourceFile *ast.SourceFile, symbol *ast.Symbol) []*ast.Node {
	if symbol == nil || sourceFile == nil {
		return nil
	}
	for index, declaration := range symbol.Declarations {
		if ast.GetSourceFileOfNode(declaration) == sourceFile {
			continue
		}
		own := make([]*ast.Node, 0, len(symbol.Declarations)-1)
		own = append(own, symbol.Declarations[:index]...)
		for _, later := range symbol.Declarations[index+1:] {
			if ast.GetSourceFileOfNode(later) == sourceFile {
				own = append(own, later)
			}
		}
		return own
	}
	return symbol.Declarations
}

// IsDeclaredOnlyInDeclarationFiles reports whether symbol has declarations and every one is in a
// declaration file: a global from a lib or an installed package, never a binding a source file
// declares. It answers about files and hands back no node, so a rule can ask it without reading another
// file's syntax, which the type-reach guard trusts.
func IsDeclaredOnlyInDeclarationFiles(symbol *ast.Symbol) bool {
	return symbol != nil && len(symbol.Declarations) > 0 && !IsDeclaredInASourceFile(symbol)
}

// IsDeclaredInASourceFile reports whether any declaration of symbol is in a source file rather than a
// declaration file: something this program declares, where a symbol the checker made with no declaration
// at all, like `undefined`, is not. It answers about files and hands back no node, which the type-reach
// guard trusts.
func IsDeclaredInASourceFile(symbol *ast.Symbol) bool {
	if symbol == nil {
		return false
	}
	for _, declaration := range symbol.Declarations {
		sourceFile := ast.GetSourceFileOfNode(declaration)
		if sourceFile == nil || !sourceFile.IsDeclarationFile {
			return true
		}
	}
	return false
}

// IsFirstDeclaredInDeclarationFile reports the source-file kind of symbol's first declaration.
// It returns only file metadata, never syntax a caller could descend into. In particular this is
// not IsDeclaredOnlyInDeclarationFiles: a merged symbol can start in a declaration file and also
// have source declarations. Callers preserving an existing first-declaration query need that order.
func IsFirstDeclaredInDeclarationFile(symbol *ast.Symbol) bool {
	if symbol == nil || len(symbol.Declarations) == 0 {
		return false
	}
	file := ast.GetSourceFileOfNode(symbol.Declarations[0])
	return file != nil && file.IsDeclarationFile
}

// IsDeclaredByADependency reports whether any declaration of symbol is a dependency's (see
// IsDependencyDeclaration): a type or value a framework or package names, which a rule about our own
// naming must leave alone, and a fix must never rename, since the name is not ours to change. One such
// declaration is enough, so a global our source merges into is still the dependency's. It answers about
// files and hands back no node, which the type-reach guard trusts.
func IsDeclaredByADependency(symbol *ast.Symbol) bool {
	if symbol == nil {
		return false
	}
	for _, declaration := range symbol.Declarations {
		if IsDependencyDeclaration(declaration) {
			return true
		}
	}
	return false
}

// IsDependencyDeclaration reports whether declaration sits outside our source: in a declaration file,
// like Next's generated route types or a lib, or in any file under a node_modules directory, which an
// installed package may ship as plain TypeScript. Ownership is decided by where the type is declared,
// not by how it is spelled (#kk5kk04).
func IsDependencyDeclaration(declaration *ast.Node) bool {
	file := ast.GetSourceFileOfNode(declaration)
	return file != nil && (file.IsDeclarationFile ||
		strings.Contains(strings.ReplaceAll(file.FileName().AsString(), "\\", "/"), "/node_modules/"))
}

// IsDeclaredInPackage reports whether any declaration of symbol sits in a file of the installed package
// named packageName, such as `@types/react`. A file's package is read from its path the way the
// compiler's own modulespecifiers.GetPackageNameFromDirectory reads it, which the shim does not export:
// the name after the last `/node_modules/`, two segments when scoped. The last one, so a pnpm store's
// `node_modules/.pnpm/@types+react@19.3.0/node_modules/@types/react/index.d.ts` is `@types/react`, and
// the whole name, so `@types/react-dom` is not `@types/react`. The compiler keeps no other map from a file
// to its package (see checking.typeDeclaredInDeclarationFile). It answers about files and hands back no
// node, which the type-reach guard trusts.
func IsDeclaredInPackage(symbol *ast.Symbol, packageName string) bool {
	if symbol == nil {
		return false
	}
	for _, declaration := range symbol.Declarations {
		file := ast.GetSourceFileOfNode(declaration)
		if file != nil && packageOfFileName(file.FileName().AsString()) == packageName {
			return true
		}
	}
	return false
}

// packageOfFileName is the installed package a file belongs to, or "" for a file under no node_modules.
func packageOfFileName(fileName string) string {
	normalized := strings.ReplaceAll(fileName, "\\", "/")
	index := strings.LastIndex(normalized, "/node_modules/")
	if index < 0 {
		return ""
	}
	rest := normalized[index+len("/node_modules/"):]
	if rest == "" || rest[0] == '.' {
		return ""
	}
	name, path, _ := strings.Cut(rest, "/")
	if name[0] != '@' {
		return name
	}
	scope, _, _ := strings.Cut(path, "/")
	if scope == "" {
		return ""
	}
	return name + "/" + scope
}

// HasDeclarations reports whether the checker supplied any declarations, without exposing syntax.
func HasDeclarations(symbol *ast.Symbol) bool {
	return symbol != nil && len(symbol.Declarations) != 0
}

// IsDeclaredInANonDeclarationFile reports whether any declaration with a source file is non-ambient.
// Unlike IsDeclaredInASourceFile it ignores missing files, preserving identifierIsShadowed's query.
func IsDeclaredInANonDeclarationFile(symbol *ast.Symbol) bool {
	if symbol == nil {
		return false
	}
	for _, declaration := range symbol.Declarations {
		file := ast.GetSourceFileOfNode(declaration)
		if file != nil && !file.IsDeclarationFile {
			return true
		}
	}
	return false
}

// IsResolvedSymbol reports whether a name resolves to a declaration. An absent symbol,
// an unresolved import's unknown symbol, and synthetic symbols without declarations
// establish no ownership: naming rules must neither report nor rename them. Follow
// aliases first, since an import declaration alone does not prove its target exists.
// This scalar query exposes no foreign syntax to a type-reach caller.
func IsResolvedSymbol(typeChecker *checker.Checker, symbol *ast.Symbol) bool {
	if typeChecker == nil || symbol == nil {
		return false
	}
	resolved := symbol
	if symbol.Flags&ast.SymbolFlagsAlias != 0 {
		resolved = checker.SkipAlias(symbol, typeChecker)
	}
	return resolved != nil && !typeChecker.IsUnknownSymbol(resolved) && HasDeclarations(resolved)
}

// IsUnresolvedType recognizes the checker's error recovery type for a missing
// contract. It is distinct from explicit any, unknown, and an absent contextual
// type (an unconstrained literal). Type queries may have already lost the missing
// name's symbol; naming rules still cannot infer ownership from that error.
func IsUnresolvedType(valueType *checker.Type) bool {
	return valueType != nil &&
		valueType.Flags()&checker.TypeFlagsAny != 0 &&
		valueType.AsIntrinsicType().IntrinsicName() == "error"
}
