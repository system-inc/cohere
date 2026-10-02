package css

// The printer object of src/language-css/printer-postcss.js, its embed.js, and the two entries the rest of
// cohere calls: Format for a .css file, and PrintToDoc for CSS embedded in another language, which is
// upstream's textToDoc (src/main/multiparser.js).
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
	"github.com/system-inc/cohere/internal/format/prettier"
	"github.com/system-inc/cohere/internal/format/printing"
)

// postcssPrinter is the printer object as the core normalizes it (src/main/parser-and-printer.js): print
// answers front matter itself, and both visitor-key functions give front matter none.
var postcssPrinter = &printing.Printer[*estree.Node]{
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
func embed(path *astPath, _ *printerOptions) func(printing.TextToDoc, printing.PrintFunc, *astPath, *printerOptions) (doc.Doc, error) {
	node := currentNode(path)
	if !isFrontMatter(node) || !supportedEmbedFrontMatterLanguages[node.String("language")] {
		return nil
	}

	return func(textToDoc printing.TextToDoc, _ printing.PrintFunc, _ *astPath, _ *printerOptions) (doc.Doc, error) {
		language := node.String("language")
		value := trim(node.String("value"))

		var embedded doc.Doc = doc.Text("")
		if value != "" {
			if language != "yaml" {
				return nil, nil
			}
			formatted, err := textToDoc(value, language)
			if err != nil {
				return nil, err
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

// settingsOf is the resolved Prettier options a format carries in options.Settings.
func settingsOf(options *printerOptions) prettier.Options {
	return options.Settings.(prettier.Options)
}

// Format is Prettier's format for a CSS file: parse, print to a doc, lay it out. Byte order marks and
// line endings are normalized by the shared layer, not here.
func Format(text string, prettierOptions prettier.Options) (formatted string, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			formatted, err = "", fmt.Errorf("css: %v", recovered)
		}
	}()

	document, err := printToDoc(text, prettierOptions)
	if err != nil {
		return "", err
	}
	return doc.Print(document, doc.Options{
		PrintWidth: prettierOptions.PrintWidth,
		TabWidth:   prettierOptions.TabWidth,
		UseTabs:    prettierOptions.UseTabs,
	}), nil
}

// PrintToDoc is upstream's textToDoc for CSS embedded in another language: the printed doc with its
// trailing hardline stripped.
func PrintToDoc(text string, prettierOptions prettier.Options) (document doc.Doc, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			document, err = nil, fmt.Errorf("css: %v", recovered)
		}
	}()

	document, err = printToDoc(text, prettierOptions)
	if err != nil {
		return nil, err
	}
	return doc.StripTrailingHardline(document), nil
}

// printToDoc is parser-postcss.js's parseCss followed by printAstToDoc (src/main/ast-to-doc.js).
func printToDoc(text string, prettierOptions prettier.Options) (doc.Doc, error) {
	root, err := parse(text)
	if err != nil {
		if !printing.IsSyntax(err) {
			err = printing.Syntax(err)
		}
		return nil, err
	}

	// Format has no textToDoc to reach the yaml printer with, and the core would drop a failing embed
	// and print the front matter as written, which is not what the fork prints. So it refuses instead.
	if frontMatter := root.Child("frontMatter"); frontMatter.String("language") == "yaml" && trim(frontMatter.String("value")) != "" {
		return nil, fmt.Errorf("css: yaml front matter is formatted by the yaml printer, which css.Format cannot reach")
	}

	printOptions := &printerOptions{
		Printer:                    postcssPrinter,
		OriginalText:               text,
		Settings:                   prettierOptions,
		EmbeddedLanguageFormatting: "auto",
	}
	return printing.PrintAstToDoc(root, nil, printOptions)
}
