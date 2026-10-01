package estree

import (
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

// ParseSourceFile parses one file with typescript-go, choosing the script kind by extension.
func ParseSourceFile(fileName string, text string) *ast.SourceFile {
	scriptKind := core.ScriptKindTS
	switch strings.ToLower(filepath.Ext(fileName)) {
	case ".tsx":
		scriptKind = core.ScriptKindTSX
	case ".jsx":
		scriptKind = core.ScriptKindJSX
	case ".js", ".mjs", ".cjs":
		scriptKind = core.ScriptKindJS
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
