package javascript

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/system-inc/cohere/internal/format/doc"
	"github.com/system-inc/cohere/internal/format/estree"
	"github.com/system-inc/cohere/internal/format/prettier"
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
	TemplateQuasis: func(node Node) ([]Node, bool) {
		if !node.Is("TemplateLiteral") {
			return nil, false
		}
		return node.List("quasis"), true
	},
}

// Format is Prettier's format for a TypeScript file: parse, attach comments, print to a doc, and lay the
// doc out. fileName decides JSX and is what upstream's options.filepath carries.
func Format(fileName string, text string, options prettier.Options) (string, error) {
	return format(fileName, text, options, "typescript", estreePrinter)
}

// format is main/core.js's formatWithCursor for one printer: the byte order mark comes off before
// parsing and goes back on after, and carriage returns become newlines. endOfLine is always "lf" (the
// config resolver refuses anything else), so nothing converts them back.
func format(fileName string, text string, options prettier.Options, parser string,
	printer *printing.Printer[*estree.Node]) (formatted string, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			formatted, err = "", fmt.Errorf("formatting %s: %v", fileName, recovered)
		}
	}()

	const byteOrderMark = "\xef\xbb\xbf"
	hasByteOrderMark := strings.HasPrefix(text, byteOrderMark)
	text = strings.TrimPrefix(text, byteOrderMark)
	if strings.Contains(text, "\r") {
		text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	}

	var root *estree.Node
	var comments []*estree.Node
	switch parser {
	case "typescript":
		root, comments, err = estree.ParseTypeScript(fileName, text)
	case "json":
		root, comments, err = estree.ParseJSON(text, true)
	case "json-stringify":
		root, comments, err = estree.ParseJSON(text, false)
	default:
		err = fmt.Errorf("no parser %q", parser)
	}
	if err != nil {
		return "", err
	}
	printOptions := &Options{
		Printer:                    printer,
		OriginalText:               text,
		Settings:                   &settings{Options: options, FilePath: fileName, Parser: parser},
		EmbeddedLanguageFormatting: "auto",
	}
	document, err := printing.PrintAstToDoc(root, comments, printOptions)
	if err != nil {
		return "", err
	}
	formatted = doc.Print(document, doc.Options{PrintWidth: options.PrintWidth, TabWidth: options.TabWidth, UseTabs: options.UseTabs})
	if hasByteOrderMark {
		formatted = byteOrderMark + formatted
	}
	return formatted, nil
}

// FormatJSON is Prettier's format for a JSON file. The parser follows language-json/languages: the
// files package managers write (package.json, package-lock.json, composer.json) use json-stringify,
// which prints the way JSON.stringify does, and every other .json file uses json, which prints with
// the JavaScript printer.
func FormatJSON(fileName string, text string, options prettier.Options) (string, error) {
	switch filepath.Base(fileName) {
	case "package.json", "package-lock.json", "composer.json":
		return format(fileName, text, options, "json-stringify", estreeJSONPrinter)
	}
	// main/normalize-format-options.js: the json parser never prints a trailing comma.
	options.TrailingComma = "none"
	return format(fileName, text, options, "json", estreePrinter)
}

// hasPrettierIgnore is upstream's hasPrettierIgnore, utilities/is-ignored.js through printers.js.
func hasPrettierIgnore(path *Path) bool {
	return isIgnored(path)
}
