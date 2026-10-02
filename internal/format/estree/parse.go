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
// postprocess. It returns the Program and the file's comments, ready for comment attachment.
//
// Prettier is given a file path, so JSX is decided by the extension, the way typescript-estree decides
// it for a known file type: .tsx parses with JSX and .ts without. A hashbang becomes a line comment,
// upstream's replaceHashbang, before parsing; the caller keeps the original text for printing.
func ParseTypeScript(fileName string, text string) (*Node, []*Node, error) {
	sourceFile := ParseSourceFile(fileName, ReplaceHashbang(text))
	program, comments, err := Convert(sourceFile)
	if err != nil {
		return nil, nil, err
	}
	program, comments = Postprocess(program, comments, text)
	return program, comments, nil
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
func ParseJavaScript(fileName string, text string) (*Node, []*Node, error) {
	program, comments, err := ConvertForBabel(ParseSourceFile(fileName, ReplaceHashbang(text)))
	if err != nil {
		return nil, nil, err
	}
	program, comments = Postprocess(program, comments, text)
	if found := findTypeScriptNode(program); found != nil {
		return nil, nil, fmt.Errorf("%s: %s at byte %d is TypeScript syntax, which the babel parser does not read",
			fileName, found.Type(), found.Start())
	}
	return program, comments, nil
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

// ParseSourceFile parses one file with typescript-go, choosing the script kind by extension.
func ParseSourceFile(fileName string, text string) *ast.SourceFile {
	scriptKind := core.ScriptKindTS
	switch strings.ToLower(filepath.Ext(fileName)) {
	case ".tsx":
		scriptKind = core.ScriptKindTSX
	case ".jsx":
		scriptKind = core.ScriptKindJSX
	case ".js", ".mjs", ".cjs":
		// TSX, not JS: see ParseJavaScript.
		scriptKind = core.ScriptKindTSX
	}
	rooted := fileName
	if !tspath.IsRootedDiskPath(rooted) {
		rooted = "/" + strings.TrimPrefix(rooted, "/")
	}
	rooted = tspath.NormalizePath(rooted)
	return parser.ParseSourceFile(ast.SourceFileParseOptions{FileName: rooted, Path: tspath.Path(rooted)}, text, scriptKind)
}

// ReplaceHashbang is upstream's replaceHashbang: `#!` becomes `//`, the same length.
func ReplaceHashbang(text string) string {
	if strings.HasPrefix(text, "#!") {
		return "//" + text[2:]
	}
	return text
}
