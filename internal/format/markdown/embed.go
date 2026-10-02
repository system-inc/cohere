package markdown

import (
	"strings"

	"github.com/system-inc/cohere/internal/format/doc"
	"github.com/system-inc/cohere/internal/format/printing"
)

// src/language-markdown/embed.js for the markdown parser (MDX's import, export and jsx are not ported),
// and the core's front-matter embed, src/main/front-matter/embed.js, which the markdown printer opts into
// through features.experimental_frontMatterSupport.

type inferableLanguage struct {
	name       string
	aliases    []string
	extensions []string
	parser     string
}

// inferParserForLanguage is inferParser(options, { language }) (src/utilities/infer-parser.js): by name,
// then alias, then extension. The name is compared lowercased against the language as given, which is
// not lowercased.
func inferParserForLanguage(languageName string) string {
	if languageName == "" {
		return ""
	}
	for _, language := range inferableLanguages {
		if strings.ToLower(language.name) == languageName {
			return language.parser
		}
	}
	for _, language := range inferableLanguages {
		for _, alias := range language.aliases {
			if alias == languageName {
				return language.parser
			}
		}
	}
	for _, language := range inferableLanguages {
		for _, extension := range language.extensions {
			if extension == "."+languageName {
				return language.parser
			}
		}
	}
	return ""
}

type embedPrint = func(printing.TextToDoc, printing.PrintFunc, *astPath, *options) (doc.Doc, error)

func embed(path *astPath, _ *options) embedPrint {
	node := currentNode(path)

	if node.NodeType == "frontMatter" {
		return embedFrontMatter(node)
	}

	if node.NodeType != "code" {
		return nil
	}
	if node.Lang == nil || *node.Lang == "" {
		return nil
	}
	language := *node.Lang

	var parser string
	// https://shiki.style/references/engine-js-compat#supported-languages
	switch language {
	case "angular-ts":
		parser = inferParserForLanguage("typescript")
	case "angular-html":
		parser = "angular"
	default:
		parser = inferParserForLanguage(language)
	}

	if parser == "" {
		return nil
	}

	return func(textToDoc printing.TextToDoc, _ printing.PrintFunc, _ *astPath, _ *options) (doc.Doc, error) {
		// Upstream overrides the filepath for ts, typescript and tsx, so the trailing comma of type
		// parameters follows the right extension. TextToDoc here takes a parser; the native side maps
		// these languages to the matching file name.
		parserOrFile := parser
		switch language {
		case "ts", "typescript":
			parserOrFile = "dummy.ts"
		case "tsx":
			parserOrFile = "dummy.tsx"
		}

		embedded, err := textToDoc(node.Value, parserOrFile)
		if err != nil {
			return nil, err
		}

		style := fenceStyle(node.Value)
		meta := ""
		if node.Meta != nil && *node.Meta != "" {
			meta = " " + *node.Meta
		}

		return doc.MarkAsRoot(doc.Concat{
			doc.Text(style),
			doc.Text(language),
			doc.Text(meta),
			doc.Hardline,
			doc.ReplaceEndOfLine(embedded, doc.Literalline),
			doc.Hardline,
			doc.Text(style),
		}), nil
	}
}

// supportedEmbedFrontMatterLanguages is SUPPORTED_EMBED_LANGUAGES.
var supportedEmbedFrontMatterLanguages = map[string]bool{"yaml": true, "toml": true}

func embedFrontMatter(node *Node) embedPrint {
	frontMatter := node.FrontMatter
	if !supportedEmbedFrontMatterLanguages[frontMatter.Language] {
		return nil
	}

	return func(textToDoc printing.TextToDoc, _ printing.PrintFunc, _ *astPath, _ *options) (doc.Doc, error) {
		value := strings.Trim(frontMatter.Value, javaScriptSpaceText)

		var embedded doc.Doc = doc.Text("")
		if value != "" {
			parser := frontMatter.Language
			if parser != "yaml" {
				parser = inferParserForLanguage(frontMatter.Language)
			}
			if parser == "" {
				return nil, nil
			}
			formatted, err := textToDoc(value, parser)
			if err != nil {
				return nil, err
			}
			embedded = formatted
		}

		explicitLanguage := ""
		if frontMatter.ExplicitLanguage != nil {
			explicitLanguage = *frontMatter.ExplicitLanguage
		}
		var afterValue doc.Doc = doc.Text("")
		if value != "" {
			afterValue = doc.Hardline
		}
		return doc.MarkAsRoot(doc.Concat{
			doc.Text(frontMatter.StartDelimiter),
			doc.Text(explicitLanguage),
			doc.Hardline,
			embedded,
			afterValue,
			doc.Text(frontMatter.EndDelimiter),
		}), nil
	}
}
