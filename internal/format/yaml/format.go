package yaml

// The parser and Prettier's core around the printer: src/language-yaml/parser-yaml.js, and coreFormat's
// empty check (src/main/core.js). The rest of core.js's part, removing and restoring a byte order mark and
// normalizing line endings, is native.Formatter's, once for every language.

import (
	"errors"

	"github.com/system-inc/cohere/internal/format/doc"
	"github.com/system-inc/cohere/internal/format/formatoptions"
	"github.com/system-inc/cohere/internal/format/printing"
	"github.com/system-inc/cohere/internal/format/yaml/unist"
)

// Format is Prettier's format for a .yaml or .yml file: a file that is empty or only whitespace formats
// to "" without being parsed, and the rest is parsed and printed. text comes as native.Formatter hands
// it over, with no byte order mark and \n line endings. fileName is upstream's options.filepath, which the printer
// reads to print a .prettierrc as JSON through textToDoc; textToDoc may be nil.
//
// A file that does not parse fails with an error printing.IsSyntax recognizes.
func Format(fileName string, text string, options formatoptions.Options, textToDoc printing.TextToDoc) (string, error) {
	return formatWithProseWrap(fileName, text, options, "preserve", textToDoc)
}

// formatWithProseWrap is Format with proseWrap given. formatoptions.Options does not carry it and none of our
// repositories sets it, so only the tests reach always and never.
func formatWithProseWrap(fileName string, text string, options formatoptions.Options, proseWrap string, textToDoc printing.TextToDoc) (string, error) {
	// coreFormat: `if (!originalText || originalText.trim().length === 0)`, before parsing.
	if trim(text) == "" {
		return "", nil
	}
	root, err := parse(text)
	if err != nil {
		return "", err
	}
	return printFile(fileName, root, text, options, proseWrap, textToDoc)
}

// FormatDoc is the doc entry for YAML embedded in another language (markdown's front matter): parse and
// PrintDoc, as upstream's textToDoc parses the text it is given and prints it to a doc. The text is taken
// as it is, with no byte order mark or line ending normalization, as textToDoc takes it. The doc still
// ends in its trailing hardline, which the caller strips (doc.StripTrailingHardline) as upstream's
// textToDoc does. A text that does not parse fails with an error printing.IsSyntax recognizes.
func FormatDoc(text string, options formatoptions.Options, textToDoc printing.TextToDoc) (doc.Doc, error) {
	root, err := parse(text)
	if err != nil {
		return nil, err
	}
	return PrintDoc(root, text, options, textToDoc)
}

// parse is parser-yaml.js's parse: yaml-unist-parser's parse with { uniqueKeys: false }, and root.comments
// deleted, since the printer prints comments itself.
//
// Upstream turns a YAMLSyntaxError into a Prettier syntax error and rethrows anything else. Both kinds
// of failure here come from the input (yaml-unist-parser's TypeErrors are on !!pairs and !!omap shapes
// it does not expect), so both are marked as syntax errors and the format phase skips the file. A panic
// in the port itself is a bug, not malformed input, and is returned unmarked.
func parse(text string) (*unist.Node, error) {
	root, err := unist.Parse(text)
	if err != nil {
		var syntaxError *unist.SyntaxError
		var thrown *unist.ThrownError
		if errors.As(err, &syntaxError) || errors.As(err, &thrown) {
			return nil, printing.Syntax(err)
		}
		return nil, err
	}
	root.Comments = nil
	return root, nil
}
