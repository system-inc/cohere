package estree

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/parser"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
)

// ParseTypeScript is Prettier's typescript parser, src/language-js/parse/typescript.js: parse, convert,
// postprocess. It returns the Program and the file's comments, ready for comment attachment, and the text
// stripped of those comments, for the printer to share with the postprocess.
//
// Prettier is given a file path, so JSX is decided by the extension, the way typescript-estree decides
// it for a known file type: .tsx parses with JSX and .ts without. A hashbang becomes a line comment,
// upstream's replaceHashbang, before parsing; the caller keeps the original text for printing.
func ParseTypeScript(fileName string, text string, nodes *Arena) (*Node, []*Node, *StrippedText, error) {
	return ParseTypeScriptFrom(fileName, text, nil, nodes)
}

// ParseTypeScriptFrom is ParseTypeScript given a tree someone already parsed, so the file is not parsed a
// second time: the fix engine's guard parses every TypeScript file it is handed, and the formatter parsed
// the same bytes again, 0.18 GB a cold ahra run (#dk2502g).
//
// The tree is taken only where ParseSourceFile would have built the same one: the same bytes, no hashbang
// (which ParseTypeScript rewrites before parsing), the script kind this file's extension picks, and a name
// that is a declaration file exactly when this one is. Those are all the parser reads (parser/parser.go:
// the text, the kind, IsDeclarationFileName of the name, and the module-indicator options, which set a
// property of the file and no node), so a tree that matches them is the tree a fresh parse returns.
// Anything else is parsed here as before, so a nil tree, or the wrong one, costs a parse and never a
// different result.
//
// Prettier reads a .ts or .tsx file as a module first, and as commonjs only when that throws
// (parse/typescript.js's combinations). The module reading sets the file's external module indicator
// before TypeScript decides whether to reparse top-level statements in an await context, so a file with
// no import or export still reads `await (x)` as an await of x, where TypeScript's own detection reads a
// call to a function named await (#0yhe787). The module reading is ParseSourceFile's with the indicator
// forced, and the commonjs one is ParseSourceFile's, TypeScript's detection unforced. A file that fails
// both is refused with the module reading's error, the one Prettier reports.
//
// The tree handed in is parsed with TypeScript's detection, as the fix engine's guard parses, and is taken
// for the module reading only where forcing could not change it: see readsAlikeAsModule.
func ParseTypeScriptFrom(
	fileName string,
	text string,
	parsed *ast.SourceFile,
	nodes *Arena,
) (*Node, []*Node, *StrippedText, error) {
	handedIsFor := treeIsFor(fileName, text, parsed)
	moduleTree := parsed
	if !handedIsFor || !readsAlikeAsModule(parsed) {
		moduleTree = parseSourceFile(fileName, ReplaceHashbang(text), true)
	}
	program, comments, stripped, moduleError := convertTypeScript(moduleTree, text, nodes)
	// Only a file that is a module because it was forced to be one, and that names await, reads otherwise
	// as commonjs; any other failure would fail again, the same way, at the cost of a second parse.
	if moduleError == nil ||
		moduleTree.ExternalModuleIndicator != moduleTree.AsNode() ||
		!strings.Contains(text, "await") {
		return program, comments, stripped, moduleError
	}
	commonJsTree := parsed
	if !handedIsFor {
		commonJsTree = ParseSourceFile(fileName, ReplaceHashbang(text))
	}
	program, comments, stripped, commonJsError := convertTypeScript(commonJsTree, text, nodes)
	if commonJsError != nil {
		return nil, nil, nil, moduleError
	}
	return program, comments, stripped, nil
}

// readsAlikeAsModule reports whether parsed, a tree built with TypeScript's own module detection, is the tree
// the module reading builds. Forcing the indicator changes a tree only through the top-level await reparse,
// which runs for a module whose top-level statements hold an identifier named await. A declaration file is
// never reparsed, and a tree that is a module already was reparsed when it was built.
func readsAlikeAsModule(parsed *ast.SourceFile) bool {
	return parsed.IsDeclarationFile || parsed.ExternalModuleIndicator != nil || !namesAwait(parsed.AsNode())
}

// namesAwait reports whether an identifier named await is anywhere under node: what the parser records,
// statement by statement, to reparse in an await context.
func namesAwait(node *ast.Node) bool {
	if node.Kind == ast.KindIdentifier && node.Text() == "await" {
		return true
	}
	return node.ForEachChild(namesAwait)
}

// treeIsFor reports whether parsed is the tree ParseTypeScript would build for text: see ParseTypeScriptFrom.
func treeIsFor(fileName string, text string, parsed *ast.SourceFile) bool {
	return parsed != nil &&
		parsed.Text() == text &&
		parsed.ScriptKind == scriptKindOf(fileName) &&
		!strings.HasPrefix(text, "#!") &&
		tspath.IsDeclarationFileName(parsed.FileName().AsString()) == tspath.IsDeclarationFileName(fileName)
}

// convertTypeScript is the convert and postprocess half of ParseTypeScript, over a tree in hand.
func convertTypeScript(sourceFile *ast.SourceFile, text string, nodes *Arena) (*Node, []*Node, *StrippedText, error) {
	program, comments, convertError := Convert(sourceFile, nodes)
	if convertError != nil {
		return nil, nil, nil, convertError
	}
	program, comments, stripped := Postprocess(program, comments, text)
	return program, comments, stripped, nil
}

// ParseJavaScript stands in for Prettier's babel parser, which it uses for .js, .mjs, .cjs and .jsx.
//
// The file is parsed as TSX rather than as JavaScript. In JavaScript mode typescript-go reparses
// JSDoc into the tree: `@param {string}` becomes a parameter's type, `@typedef` a type alias
// statement, `@type` a cast. Babel's tree has none of that, so a converter reading typescript-go's
// would print types the source never wrote. TSX mode reparses nothing, and accepts JSX the way
// Babel does for these files.
//
// TypeScript's grammar is a superset of JavaScript's with one class of exception: text both read
// differently, chiefly `a < b > (c)`, comparisons to Babel and a generic call to TypeScript. Valid
// JavaScript never contains a TypeScript-only node, so one in the tree is either such an ambiguity
// or a file Babel would reject, and either way the file is refused rather than printed as TypeScript.
//
// The tree is typescript-estree's with Babel's one postprocess difference: see ConvertForBabel.
func ParseJavaScript(fileName string, text string, nodes *Arena) (*Node, []*Node, *StrippedText, error) {
	program, comments, convertForBabelError := ConvertForBabel(ParseSourceFile(fileName, ReplaceHashbang(text)), nodes)
	if convertForBabelError != nil {
		return nil, nil, nil, convertForBabelError
	}
	program, comments, stripped := Postprocess(program, comments, text)
	if found := findTypeScriptNode(program); found != nil {
		return nil, nil, nil, fmt.Errorf("%s: %s at byte %d is TypeScript syntax, which the babel parser does not read",
			fileName, found.Type(), found.Start())
	}
	return program, comments, stripped, nil
}

// findTypeScriptNode is the first node, depth first, whose type is one only TypeScript produces.
func findTypeScriptNode(node *Node) *Node {
	if strings.HasPrefix(node.Type(), "TS") {
		return node
	}
	for _, child := range ChildNodes(node) {
		if found := findTypeScriptNode(child); found != nil {
			return found
		}
	}
	return nil
}

// ParseSourceFile parses one file with typescript-go, choosing the script kind by extension, and leaving
// whether it is a module to TypeScript's own detection.
func ParseSourceFile(fileName string, text string) *ast.SourceFile {
	return parseSourceFile(fileName, text, false)
}

// parseSourceFile is ParseSourceFile, with the file a module whatever it holds when asModule is set:
// typescript-estree's sourceType "module", which sets every file's external module indicator.
func parseSourceFile(fileName string, text string, asModule bool) *ast.SourceFile {
	rooted := fileName
	if !tspath.IsRootedDiskPath(rooted) {
		rooted = "/" + strings.TrimPrefix(rooted, "/")
	}
	rooted = tspath.NormalizePath(rooted)
	return parser.ParseSourceFile(
		ast.SourceFileParseOptions{
			FileName:                       tspath.RootedFilePath(rooted),
			PathKey:                        tspath.PathKey(rooted),
			ExternalModuleIndicatorOptions: ast.ExternalModuleIndicatorOptions{Force: asModule},
		},
		text,
		scriptKindOf(fileName),
	)
}

// scriptKindOf is the script kind ParseSourceFile parses a file as, chosen by extension.
func scriptKindOf(fileName string) core.ScriptKind {
	switch strings.ToLower(filepath.Ext(fileName)) {
	case ".tsx":
		return core.ScriptKindTSX
	case ".jsx":
		return core.ScriptKindJSX
	case ".js", ".mjs", ".cjs":
		// TSX, not JS: see ParseJavaScript.
		return core.ScriptKindTSX
	}
	return core.ScriptKindTS
}

// ReplaceHashbang is upstream's replaceHashbang: `#!` becomes `//`, the same length.
func ReplaceHashbang(text string) string {
	if strings.HasPrefix(text, "#!") {
		return "//" + text[2:]
	}
	return text
}
