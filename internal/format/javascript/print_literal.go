package javascript

import (
	"sort"
	"strings"

	"github.com/system-inc/cohere/internal/format/estree"
)

// print/literal.js. The Babel literal types never occur in a typescript-estree tree; only Literal does.

// printLiteral is upstream's printLiteral.
func printLiteral(path *Path, options *Options) Doc {
	current := node(path)
	if regex, isRegex := current.Get("regex").(*estree.Regex); isRegex {
		return concat(printRegex(regex))
	}
	if current.Truthy("bigint") {
		return concat(printBigInt(current.String("raw")))
	}
	switch value := current.Get("value").(type) {
	case float64:
		return concat(printNumber(current.String("raw")))
	case string:
		if isDirective(path) {
			return concat(printDirective(current.String("raw"), options))
		}
		return replaceEndOfLine(printString(current.String("raw"), options))
	case bool:
		if value {
			return concat("true")
		}
		return concat("false")
	case nil:
		return concat("null")
	}
	panic("unknown literal value")
}

// isDirective is upstream's isDirective.
func isDirective(path *Path) bool {
	if keyOf(path) != "expression" {
		return false
	}
	parent := parentOf(path)
	_, isString := parent.Get("directive").(string)
	return parent.Is("ExpressionStatement") && isString
}

// printBigInt is upstream's printBigInt.
func printBigInt(raw string) string {
	return strings.ToLower(raw)
}

// printRegex is upstream's printRegex: the flags sorted.
func printRegex(regex *estree.Regex) string {
	flags := strings.Split(regex.Flags, "")
	sort.Strings(flags)
	return "/" + regex.Pattern + "/" + strings.Join(flags, "")
}

// printDirective is upstream's printDirective.
func printDirective(rawText string, options *Options) string {
	rawContent := rawText[1 : len(rawText)-1]
	if rawContent == "use strict" || !(strings.Contains(rawContent, `"`) || strings.Contains(rawContent, "'")) {
		enclosingQuote := `"`
		if settingsOf(options).SingleQuote {
			enclosingQuote = "'"
		}
		return enclosingQuote + rawContent + enclosingQuote
	}
	return rawText
}
