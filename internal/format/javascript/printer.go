package javascript

import (
	"fmt"

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
func Format(fileName string, text string, options prettier.Options) (formatted string, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			formatted, err = "", fmt.Errorf("formatting %s: %v", fileName, recovered)
		}
	}()

	program, comments, err := estree.ParseTypeScript(fileName, text)
	if err != nil {
		return "", err
	}
	printOptions := &Options{
		Printer:                    estreePrinter,
		OriginalText:               text,
		Settings:                   &settings{Options: options, FilePath: fileName},
		EmbeddedLanguageFormatting: "auto",
	}
	document, err := printing.PrintAstToDoc(program, comments, printOptions)
	if err != nil {
		return "", err
	}
	return doc.Print(document, doc.Options{PrintWidth: options.PrintWidth, TabWidth: options.TabWidth, UseTabs: options.UseTabs}), nil
}

// hasPrettierIgnore is upstream's hasPrettierIgnore, utilities/is-ignored.js through printers.js.
func hasPrettierIgnore(path *Path) bool {
	return isIgnored(path)
}
