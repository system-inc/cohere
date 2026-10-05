package javascript

import (
	"fmt"
	"path/filepath"

	"github.com/system-inc/cohere/internal/format/doc"
	"github.com/system-inc/cohere/internal/format/estree"
	"github.com/system-inc/cohere/internal/format/formatoptions"
	"github.com/system-inc/cohere/internal/format/printing"
)

// estreePrinter is upstream's printer, src/language-js/printers.js: the hooks the print core calls.
var estreePrinter = &printing.Printer[*estree.Node]{
	Print:                printEstree,
	LocStart:             estree.LocStart,
	LocEnd:               estree.LocEnd,
	VisitorKeys:          estree.VisitorKeys,
	HasPrettierIgnore:    hasPrettierIgnore,
	PrintPrettierIgnored: printEstree,
	WillPrintOwnComments: willPrintOwnComments,
	CanAttachComment:     canAttachComment,
	IsBlockComment:       isBlockComment,
	PrintComment:         printComment,
	GetCommentChildNodes: getCommentChildNodes,
	HandleComments: printing.CommentHandlers[*estree.Node]{
		OwnLine:   handleOwnLineComment,
		EndOfLine: handleEndOfLineComment,
		Remaining: handleRemainingComment,
	},
	AvoidAstMutation: true,
	Embed:            embed,
	MayHoldEmbed:     holdsTemplateLiteral,
	TemplateQuasis: func(node Node) ([]Node, bool) {
		if !node.Is("TemplateLiteral") {
			return nil, false
		}
		return node.List("quasis"), true
	},
}

// Format is Prettier's format for a TypeScript file: parse, attach comments, print to a doc, and lay the
// doc out. fileName decides JSX and is what upstream's options.filepath carries. textToDoc formats the
// languages a file embeds (GraphQL in a gql template), or is nil.
func Format(fileName string, text string, options formatoptions.Options, textToDoc printing.TextToDoc) (string, error) {
	return format(fileName, text, options, "typescript", textToDoc)
}

// FormatJavaScript is Prettier's format for a .js, .mjs, .cjs or .jsx file, which upstream parses with
// babel. The parser name matters to the printer only in print/key.js: under babel a numeric string
// key unquotes (`{ "1": a }` prints `{ 1: a }`), which TypeScript forbids.
func FormatJavaScript(fileName string, text string, options formatoptions.Options, textToDoc printing.TextToDoc) (string, error) {
	return format(fileName, text, options, "babel", textToDoc)
}

// FormatJSON is Prettier's format for a JSON file, with the parser JSONParser picks. JSON embeds
// nothing.
func FormatJSON(fileName string, text string, options formatoptions.Options) (string, error) {
	return format(fileName, text, options, JSONParser(fileName), nil)
}

// JSONParser is the parser language-json/languages gives a .json file: the files package managers
// write (package.json, package-lock.json, composer.json) use json-stringify, which prints the way
// JSON.stringify does, and every other .json file uses json, which prints with the JavaScript printer.
func JSONParser(fileName string) string {
	switch filepath.Base(fileName) {
	case "package.json", "package-lock.json", "composer.json":
		return "json-stringify"
	}
	return "json"
}

// format parses, prints and lays out one file. Byte order marks and line endings, main/core.js's part,
// are normalized by the caller, native.Formatter, once for every printer.
func format(fileName string, text string, options formatoptions.Options, parser string, textToDoc printing.TextToDoc) (string, error) {
	document, err := printToDoc(fileName, text, options, parser, "", textToDoc, nil)
	if err != nil {
		return "", err
	}
	return doc.Print(document, doc.Options{PrintWidth: options.PrintWidth, TabWidth: options.TabWidth, UseTabs: options.UseTabs}), nil
}

// PrintToDoc is upstream's textToDoc (src/main/multiparser.js) for a JavaScript-family parser: the doc
// for text embedded in another language's file, with its trailing hardline stripped, for the outer
// printer to lay out at its own indentation. parser is "typescript", "babel", "json" or
// "json-stringify"; parentParser is the outer file's parser, which upstream sets on every embed.
func PrintToDoc(fileName string, text string, options formatoptions.Options, parser string, parentParser string,
	textToDoc printing.TextToDoc) (doc.Doc, error) {
	// The doc is laid out later, by the outer printer, so its tree outlives this call and comes from the heap.
	document, err := printToDoc(fileName, text, options, parser, parentParser, textToDoc, nil)
	if err != nil {
		return nil, err
	}
	return doc.StripTrailingHardline(document), nil
}

// printToDoc parses with the parser and prints the tree to a doc.
//
// The tree's nodes come from nodes, or the heap when it is nil. A caller that passes an arena releases it
// only once the doc is laid out, since the doc is printed from the tree.
func printToDoc(fileName string, text string, options formatoptions.Options, parser string, parentParser string,
	textToDoc printing.TextToDoc, nodes *estree.Arena) (document doc.Doc, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			document, err = nil, fmt.Errorf("formatting %s: %v", fileName, recovered)
		}
	}()

	var root *estree.Node
	var comments []*estree.Node
	printer := estreePrinter
	switch parser {
	case "typescript":
		root, comments, err = estree.ParseTypeScript(fileName, text, nodes)
	case "babel":
		root, comments, err = estree.ParseJavaScript(fileName, text, nodes)
	case "json":
		root, comments, err = estree.ParseJSON(text, true)
		// main/normalize-format-options.js: the json parser never prints a trailing comma.
		options.TrailingComma = "none"
	case "json-stringify":
		root, comments, err = estree.ParseJSON(text, false)
		printer = estreeJSONPrinter
	default:
		err = fmt.Errorf("no parser %q", parser)
	}
	if err != nil {
		return nil, err
	}
	printOptions := &Options{
		Printer:                    printer,
		OriginalText:               text,
		Settings:                   &settings{Options: options, FilePath: fileName, Parser: parser, ParentParser: parentParser},
		EmbeddedLanguageFormatting: "auto",
		TextToDoc:                  textToDoc,
	}
	return printing.PrintAstToDoc(root, comments, printOptions)
}

// hasPrettierIgnore is upstream's hasPrettierIgnore, utilities/is-ignored.js through printers.js.
func hasPrettierIgnore(path *Path) bool {
	return isIgnored(path)
}
