package css

// The printer object of src/language-css/printer-postcss.js, its embed.js, and the entries the rest of
// cohere calls: Format for a .css file and FormatSCSS for a .scss one, and PrintToDoc and PrintToDocSCSS
// for CSS or SCSS embedded in another language, which is upstream's textToDoc (src/main/multiparser.js).
// The scss entries are the css ones with options.parser "scss", which the glue and the printer read
// where upstream does.
//
// The printer sets print, embed, getVisitorKeys, insertPragma and massageAstNode, and opts into the core's
// front matter support (features.experimental_frontMatterSupport). It sets no comment hooks: postcss
// comments are nodes in the tree, printed where they stand, so there is nothing for the core to attach
// and the comments list handed to the core is empty. massageAstNode is test-only upstream and
// insertPragma runs only under insertPragma, which cohere never sets, so neither is here.

import (
	"fmt"

	"github.com/system-inc/cohere/internal/format/doc"
	"github.com/system-inc/cohere/internal/format/estree"
	"github.com/system-inc/cohere/internal/format/format_options"
	"github.com/system-inc/cohere/internal/format/printing"
)

// postcssPrinter is the printer object as the core normalizes it (src/main/parser-and-printer.js): print
// answers front matter itself, and both visitor-key functions give front matter none.
var postcssPrinter = &printing.Printer[*estree.Node, printSettings, printing.NoArguments]{
	Print:            genericPrint,
	LocStart:         locStart,
	LocEnd:           locEnd,
	VisitorKeys:      printerVisitorKeys,
	Embed:            embed,
	EmbedVisitorKeys: embedVisitorKeys,
}

// printerVisitorKeys is getVisitorKeys (get-visitor-keys.js over visitor-keys.evaluate.js, which the glue
// ports as visitorKeys) behind createGetVisitorKeysFunction's front matter check.
func printerVisitorKeys(node *estree.Node) []string {
	if isFrontMatter(node) {
		return nil
	}
	return visitorKeys(node)
}

// embedVisitorKeys is embed.getVisitorKeys: `front-matter` is only available on `css-root`.
func embedVisitorKeys(node *estree.Node) []string {
	if isFrontMatter(node) {
		return nil
	}
	if node.Type() == "css-root" {
		return []string{"frontMatter"}
	}
	return nil
}

// supportedEmbedFrontMatterLanguages is src/main/front-matter/embed.js's SUPPORTED_EMBED_LANGUAGES.
var supportedEmbedFrontMatterLanguages = map[string]bool{"yaml": true, "toml": true}

// embed is embed.js, an empty function, behind the core's front matter embed
// (src/main/front-matter/embed.js): yaml front matter is formatted by the yaml printer through textToDoc.
// toml has no parser in Prettier, so inferParser finds none and it prints as written.
func embed(
	path *astPath,
	_ *printerOptions,
) func(printing.TextToDoc, printFunc, *astPath, *printerOptions) (doc.Doc, error) {
	node := currentNode(path)
	if !isFrontMatter(node) || !supportedEmbedFrontMatterLanguages[node.String("language")] {
		return nil
	}

	return func(textToDoc printing.TextToDoc, _ printFunc, _ *astPath, _ *printerOptions) (doc.Doc, error) {
		language := node.String("language")
		value := trim(node.String("value"))

		var embedded doc.Doc = doc.Text("")
		if value != "" {
			if language != "yaml" {
				return nil, nil
			}
			formatted, textToDocError := textToDoc(value, language)
			if textToDocError != nil {
				return nil, textToDocError
			}
			embedded = formatted
		}

		return doc.MarkAsRoot(doc.Concat{
			doc.Text(node.String("startDelimiter")),
			doc.Text(node.String("explicitLanguage")),
			doc.Hardline,
			embedded,
			choose(value != "", func() doc.Doc { return doc.Hardline }),
			doc.Text(node.String("endDelimiter")),
		}), nil
	}
}

// printSettings is what a format carries in options.Settings: the resolved Prettier options, and
// options.parser, which format_options.Options does not hold since the file's language decides it.
type printSettings struct {
	format_options.Options
	// parser is options.parser: "css" or "scss".
	parser string
}

// settingsOf is the resolved Prettier options a format carries in options.Settings.
func settingsOf(options *printerOptions) format_options.Options {
	return printSettingsOf(options).Options
}

// parserOf is options.parser.
func parserOf(options *printerOptions) string {
	return printSettingsOf(options).parser
}

// printSettingsOf is the printSettings a format carries in options.Settings.
func printSettingsOf(options *printerOptions) printSettings {
	return options.Settings
}

// Format is Prettier's format for a CSS file: parse, print to a doc, lay it out. Byte order marks and
// line endings are normalized by the shared layer, not here. textToDoc formats yaml front matter with the
// yaml printer, as upstream's embed does; with none, a file holding yaml front matter is refused.
func Format(text string, prettierOptions format_options.Options, textToDoc printing.TextToDoc) (string, error) {
	return formatWithParser(text, prettierOptions, "css", textToDoc)
}

// FormatScss is Prettier's format for an SCSS file, the parser "scss".
func FormatScss(text string, prettierOptions format_options.Options, textToDoc printing.TextToDoc) (string, error) {
	return formatWithParser(text, prettierOptions, "scss", textToDoc)
}

// formatWithParser is Format under the parser named, "css" or "scss".
func formatWithParser(
	text string,
	prettierOptions format_options.Options,
	parser string,
	textToDoc printing.TextToDoc,
) (formatted string, formatWithParserError error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			formatted, formatWithParserError = "", fmt.Errorf("%s: %v", parser, recovered)
		}
	}()

	document, formatWithParserError := printToDoc(text, prettierOptions, parser, textToDoc)
	if formatWithParserError != nil {
		return "", formatWithParserError
	}
	return doc.Print(document, doc.Options{
		PrintWidth:     prettierOptions.PrintWidth,
		TabWidth:       prettierOptions.TabWidth,
		UseTabs:        prettierOptions.UseTabs,
		ExpectedLength: len(text),
	}), nil
}

// PrintToDoc is upstream's textToDoc for CSS embedded in another language: the printed doc with its
// trailing hardline stripped. textToDoc is as Format takes it.
func PrintToDoc(text string, prettierOptions format_options.Options, textToDoc printing.TextToDoc) (doc.Doc, error) {
	return printToDocWithParser(text, prettierOptions, "css", textToDoc)
}

// PrintToDocScss is PrintToDoc for SCSS, the parser "scss".
func PrintToDocScss(
	text string,
	prettierOptions format_options.Options,
	textToDoc printing.TextToDoc,
) (doc.Doc, error) {
	return printToDocWithParser(text, prettierOptions, "scss", textToDoc)
}

// printToDocWithParser is PrintToDoc under the parser named, "css" or "scss".
func printToDocWithParser(
	text string,
	prettierOptions format_options.Options,
	parser string,
	textToDoc printing.TextToDoc,
) (document doc.Doc, printToDocWithParserError error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			document, printToDocWithParserError = nil, fmt.Errorf("%s: %v", parser, recovered)
		}
	}()

	document, printToDocWithParserError = printToDoc(text, prettierOptions, parser, textToDoc)
	if printToDocWithParserError != nil {
		return nil, printToDocWithParserError
	}
	return doc.StripTrailingHardline(document), nil
}

// printToDoc is parser-postcss.js's parseCss (or parseScss) followed by printAstToDoc
// (src/main/ast-to-doc.js).
func printToDoc(
	text string,
	prettierOptions format_options.Options,
	parser string,
	textToDoc printing.TextToDoc,
) (doc.Doc, error) {
	root, parseError := parseWithParserName(text, parser)
	if parseError != nil {
		if !printing.IsSyntax(parseError) {
			parseError = printing.Syntax(parseError)
		}
		return nil, parseError
	}

	// With no textToDoc to reach the yaml printer, the core would drop the failing embed and print the front
	// matter as written, which is not what the fork prints. So it refuses instead.
	if frontMatter := root.Child(
		"frontMatter",
	); textToDoc == nil && frontMatter.String("language") == "yaml" && trim(frontMatter.String("value")) != "" {
		return nil, fmt.Errorf(
			"%s: yaml front matter is formatted by the yaml printer, which css.Format cannot reach",
			parser,
		)
	}

	printOptions := &printerOptions{
		Printer:                    postcssPrinter,
		OriginalText:               text,
		Settings:                   printSettings{Options: prettierOptions, parser: parser},
		EmbeddedLanguageFormatting: "auto",
		TextToDoc:                  textToDoc,
	}
	return printing.PrintAstToDoc(root, nil, printOptions)
}
