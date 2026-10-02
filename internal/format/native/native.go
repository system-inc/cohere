// Package native is cohere's own formatter: the printers that replace Prettier, routed by file type.
//
// It is the candidate the differential measures as "native", and the formatter the format phase
// switches to once every corpus reads 100%. Until a language's printer exists, files of that type are
// refused rather than passed through, so the harness reads 0% for them instead of crediting a printer
// that does not exist.
//
// # One file per printer, so two Circles never share one
//
// Printers register from their own file in this package, `typescript.go` and `markdown.go`, each
// calling Register in an init function. Two Circles port in parallel, and a shared routing table would
// hold both of their uncommitted edits at once, so a commit by pathspec would carry the other's work.
// A file each keeps every commit to its owner's lines.
package native

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/system-inc/cohere/internal/format/doc"
	"github.com/system-inc/cohere/internal/format/prettier"
	"github.com/system-inc/cohere/internal/format/printing"
)

// Print formats one file's text with the options its repository resolves to.
//
// fileName is passed because a printer may need it: Prettier picks json-stringify for package.json by
// name, not by extension.
type Print func(fileName string, text string, options prettier.Options) (string, error)

// PrintDoc is a printer's textToDoc (src/main/multiparser.js): the doc for text embedded in another
// language's file, with the trailing hardline stripped, for the outer printer to lay out at its own
// indentation. parser is the one the embedding asked for (a fence's language can pick json for a file
// named anything), parentParser is the outer file's, and textToDoc formats what this text embeds in turn.
type PrintDoc func(fileName string, text string, options prettier.Options, parser string, parentParser string,
	textToDoc printing.TextToDoc) (doc.Doc, error)

var (
	mutex       sync.RWMutex
	printers    = map[string]Print{}
	docPrinters = map[string]PrintDoc{}
)

// RegisterDoc routes an extension to a printer's doc entry, for embedding. A printer without one is
// embedded through its Print, as text.
func RegisterDoc(extension string, printDoc PrintDoc) {
	mutex.Lock()
	defer mutex.Unlock()
	extension = strings.ToLower(extension)
	if _, taken := docPrinters[extension]; taken {
		panic(fmt.Sprintf("native: %s is registered twice for embedding", extension))
	}
	docPrinters[extension] = printDoc
}

// embeddedFileNames maps the parser names embeds ask for to a file name the registered printers route
// by. A parser with no native printer yet is refused, and the core then leaves the embedded text as
// written, which is what upstream does when an embedded format fails.
var embeddedFileNames = map[string]string{
	"typescript":     "embedded.tsx",
	"babel":          "embedded.js",
	"json":           "embedded.json",
	"json-stringify": "embedded.json",
	"json5":          "embedded.json5",
	"jsonc":          "embedded.jsonc",
	"yaml":           "embedded.yaml",
	"graphql":        "embedded.graphql",
	"css":            "embedded.css",
	"scss":           "embedded.scss",
	"less":           "embedded.less",
	"markdown":       "embedded.md",
}

// embeddedParsers is the parser a file name an embed passes resolves to, for the embeds that override
// the file path rather than name a parser: markdown's ts and tsx fences, whose trailing comma of type
// parameters follows the extension.
var embeddedParsers = map[string]string{
	".ts":  "typescript",
	".tsx": "typescript",
	".js":  "babel",
	".md":  "markdown",
}

// TextToDoc is upstream's textToDoc for a file whose parser is parentParser: what an embedding printer
// calls to format another language's text. The argument is a parser name, or a file name for the
// embeds that override the path. Every embed passes parentParser on, as main/multiparser.js does.
//
// A language with a doc entry (RegisterDoc) returns its doc, which the outer printer lays out at its
// own indentation, as upstream does. One with only a Print returns its formatted text as a single
// text doc, which is laid out at the full width even when nested: that differs from upstream only on
// a line within the nesting's indentation of the limit.
func TextToDoc(options prettier.Options, parentParser string) printing.TextToDoc {
	return func(text string, parserOrFile string) (doc.Doc, error) {
		fileName, parser := parserOrFile, parserOrFile
		if strings.Contains(parserOrFile, ".") {
			parser = embeddedParsers[strings.ToLower(filepath.Ext(parserOrFile))]
		} else {
			fileName = embeddedFileNames[parserOrFile]
		}
		extension := strings.ToLower(filepath.Ext(fileName))
		mutex.RLock()
		printDoc := docPrinters[extension]
		mutex.RUnlock()
		if printDoc != nil {
			return printDoc(fileName, text, options, parser, parentParser, TextToDoc(options, parser))
		}
		formatted, err := Formatter{Options: options}.Format(fileName, text)
		if err != nil {
			return nil, err
		}
		// textToDoc strips the trailing hardline the embedded printer ends with.
		return doc.Text(strings.TrimSuffix(formatted, "\n")), nil
	}
}

// Register routes an extension (".tsx", with the dot) to a printer. Registering one twice is a
// programming error, because two printers claiming a file type would make the result depend on init
// order, so it panics at startup rather than formatting with whichever won.
func Register(extension string, print Print) {
	mutex.Lock()
	defer mutex.Unlock()
	extension = strings.ToLower(extension)
	if _, taken := printers[extension]; taken {
		panic(fmt.Sprintf("native: %s is registered twice", extension))
	}
	printers[extension] = print
}

// Extensions lists what is registered, for a report to say which languages the native side covers.
func Extensions() []string {
	mutex.RLock()
	defer mutex.RUnlock()
	extensions := make([]string, 0, len(printers))
	for extension := range printers {
		extensions = append(extensions, extension)
	}
	sort.Strings(extensions)
	return extensions
}

// Formatter formats with the registered printers and one set of options.
type Formatter struct {
	Options prettier.Options
}

// Handles reports whether a printer is registered for the file's type.
func (formatter Formatter) Handles(fileName string) bool {
	_, present := lookup(fileName)
	return present
}

// Format formats a file, or refuses one no printer handles.
func (formatter Formatter) Format(fileName string, text string) (string, error) {
	print, present := lookup(fileName)
	if !present {
		return "", fmt.Errorf("native: no printer for %s yet", fileName)
	}
	return print(fileName, text, formatter.Options)
}

func lookup(fileName string) (Print, bool) {
	mutex.RLock()
	defer mutex.RUnlock()
	print, present := printers[strings.ToLower(filepath.Ext(fileName))]
	return print, present
}
