package native

import (
	"fmt"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/format/formatoptions"
	ecmascripttext "github.com/system-inc/cohere/internal/lint/ecmascript/text"
)

// Formatter formats with the registered printers and one set of options.
type Formatter struct {
	Options formatoptions.Options
}

// Handles reports whether a printer is registered for the file's type.
func (formatter Formatter) Handles(fileName string) bool {
	_, present := lookup(fileName)
	return present
}

// Format formats a file, or refuses one no printer handles.
func (formatter Formatter) Format(fileName string, text string) (string, error) {
	return formatter.FormatParsed(fileName, text, nil)
}

// FormatParsed is Format with a tree of the text someone already parsed, or nil. A printer that parses
// with typescript-go takes it where it is the tree it would build, and every other printer ignores it.
func (formatter Formatter) FormatParsed(fileName string, text string, parsed *ast.SourceFile) (string, error) {
	print, present := lookup(fileName)
	if !present {
		return "", fmt.Errorf("native: no printer for %s yet", fileName)
	}

	// main/core.js's formatWithCursor, once for every language as upstream does it: the byte order mark
	// comes off before parsing and goes back on after, and carriage returns become newlines. endOfLine
	// is always "lf" (formatoptions.Resolve refuses anything else), so nothing converts them back.
	hasByteOrderMark := strings.HasPrefix(text, byteOrderMark)
	text = strings.TrimPrefix(text, byteOrderMark)
	if strings.Contains(text, "\r") {
		text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	}

	// The tree is of the text as given, so a byte order mark or a carriage return taken off above leaves it
	// describing other bytes, and the printer is then not offered it. It would refuse it anyway, by comparing
	// texts, but not handing it over says why.
	var formatted string
	var err error
	if ecmascripttext.TrimWhitespace(text) == "" {
		// core.js's coreFormat: empty or whitespace-only text formats to nothing before any parser sees it,
		// in every language. A parser that refuses empty input (JSON's) is never asked (#w6vtsrn).
		formatted = ""
	} else if printParsed, takesTree := lookupParsed(fileName); takesTree && parsed != nil && parsed.Text() == text {
		formatted, err = printParsed(fileName, text, parsed, formatter.Options)
	} else {
		formatted, err = print(fileName, text, formatter.Options)
	}
	if err != nil {
		return "", err
	}
	if hasByteOrderMark {
		formatted = byteOrderMark + formatted
	}
	return formatted, nil
}
