package javascript

import (
	"encoding/json"
	"regexp"
	"strconv"
	"unicode"

	"github.com/system-inc/cohere/internal/format/printing"
)

// print/return-statement.js, variable-declaration.js, sequence-expression.js, await-expression.js,
// rest-element.js, property.js and key.js.

// printReturnOrThrowArgument is upstream's printReturnOrThrowArgument. experimentalTernaries is never
// set in our configurations, so its branch is not taken.
func printReturnOrThrowArgument(path *Path, options *Options, print PrintFunc) Doc {
	current := node(path)
	argumentDoc := print(nil, nil)
	if returnArgumentHasLeadingComment(current, options) {
		return concat("(", indent(concat(hardline, argumentDoc)), hardline, ")")
	}
	if isBinaryish(current) {
		return group(concat(ifBreak("(", ""), indent(concat(softline, argumentDoc)), softline, ifBreak(")", "")))
	}
	return argumentDoc
}

// printReturnOrThrowStatement is upstream's printReturnOrThrowStatement, exported upstream as
// printReturnStatement and printThrowStatement.
func printReturnOrThrowStatement(path *Path, options *Options, print PrintFunc) Doc {
	current := node(path)
	keyword := "return"
	if current.Is("ThrowStatement") {
		keyword = "throw"
	}
	var argument Doc = emptyDoc
	if current.Child("argument") != nil {
		argument = concat(" ", call(path, func(path *Path) Doc {
			return printReturnOrThrowArgument(path, options, print)
		}, "argument"))
	}
	return concat(keyword, argument, printSemicolon(options))
}

func printReturnStatement(path *Path, options *Options, print PrintFunc) Doc {
	return printReturnOrThrowStatement(path, options, print)
}

func printThrowStatement(path *Path, options *Options, print PrintFunc) Doc {
	return printReturnOrThrowStatement(path, options, print)
}

// printVariableDeclaration is upstream's printVariableDeclaration.
func printVariableDeclaration(path *Path, options *Options, print PrintFunc) Doc {
	current := node(path)
	printed := printAll(path, print, "declarations")
	key := keyOf(path)
	parent := parentOf(path)
	isForXStatementInitializer := key == "init" && parent.Is("ForStatement") ||
		key == "left" && parent.Is("ForInStatement", "ForOfStatement")

	declarations := current.List("declarations")
	hasValue := false
	for _, declarator := range declarations {
		if declarator.Child("init") != nil {
			hasValue = true
			break
		}
	}

	var firstVariable Doc
	if len(printed) == 1 && !hasAnyComment(declarations[0]) {
		firstVariable = printed[0]
	} else if len(printed) > 0 {
		firstVariable = indent(printed[0])
	}

	var first Doc = emptyDoc
	if firstVariable != nil {
		first = concat(" ", firstVariable)
	}
	rest := make([]any, 0, len(printed))
	for _, declaration := range printed[min(1, len(printed)):] {
		separator := line
		if hasValue && !isForXStatementInitializer {
			separator = hardline
		}
		rest = append(rest, concat(",", separator, declaration))
	}
	var semicolon Doc = emptyDoc
	if !isForXStatementInitializer {
		semicolon = printSemicolon(options)
	}
	return group(concat(
		printDeclareToken(path),
		current.String("kind"),
		first,
		indent(concat(rest...)),
		semicolon,
	))
}

// shouldIndentSequenceExpression is upstream's shouldIndentSequenceExpression.
func shouldIndentSequenceExpression(path *Path, options *Options) bool {
	key := keyOf(path)
	parent := parentOf(path)
	if key == "argument" && isReturnOrThrowStatement(parent) && needsParentheses(path, options) {
		return true
	}
	return key == "body" && parent.Is("ArrowFunctionExpression")
}

// printSequenceExpression is upstream's printSequenceExpression.
func printSequenceExpression(path *Path, options *Options, print PrintFunc) Doc {
	parent := parentOf(path)
	if parent.Is("ExpressionStatement", "ForStatement") {
		var parts []any
		each(path, func(path *Path, _ int) {
			if path.IsFirst() {
				parts = append(parts, print(nil, nil))
			} else {
				parts = append(parts, ",", indent(concat(line, print(nil, nil))))
			}
		}, "expressions")
		return group(concat(parts...))
	}
	parts := join(concat(",", line), printAll(path, print, "expressions"))
	if shouldIndentSequenceExpression(path, options) {
		return group(ifBreak(concat(indent(concat(softline, parts)), softline), parts))
	}
	return group(parts)
}

// printAwaitExpression is upstream's printAwaitExpression.
func printAwaitExpression(path *Path, options *Options, print PrintFunc) Doc {
	current := node(path)
	parts := []any{"await"}
	if current.Child("argument") != nil {
		parts = append(parts, " ", print("argument", nil))
		parent := parentOf(path)
		if isCallExpression(parent) && parent.Child("callee") == current ||
			isMemberExpression(parent) && parent.Child("object") == current {
			parts = []any{indent(concat(append([]any{softline}, parts...)...)), softline}
			// avoid printing `await (await` on one line
			parentAwaitOrBlock, _ := path.FindAncestor(func(node Node) bool {
				return node.Is("AwaitExpression", "BlockStatement")
			})
			if !parentAwaitOrBlock.Is("AwaitExpression") ||
				!startsWithNoLookaheadToken(parentAwaitOrBlock.Child("argument"), func(leftmost Node) bool {
					return leftmost == current
				}) {
				return group(concat(parts...))
			}
		}
	}
	return concat(parts...)
}

// printRestOrSpreadElement is upstream's printRestOrSpreadElement, exported upstream as
// printRestElement and printSpreadElement.
func printRestOrSpreadElement(path *Path, print PrintFunc) Doc {
	return concat("...", print("argument", nil), printTypeAnnotationProperty(path, print))
}

func printRestElement(path *Path, print PrintFunc) Doc { return printRestOrSpreadElement(path, print) }
func printSpreadElement(path *Path, print PrintFunc) Doc {
	return printRestOrSpreadElement(path, print)
}

// printProperty is upstream's printProperty: Property and ImportAttribute.
func printProperty(path *Path, options *Options, print PrintFunc) Doc {
	current := node(path)
	if current.Bool("shorthand") {
		return print("value", nil)
	}
	return printAssignment(path, options, print, printKey(path, options, print), ":", "value")
}

// print/key.js. Our parser is always "typescript" and quoteProps is always "as-needed".

func isTsEnumMember(node Node) bool { return node.Is("TSEnumMember") }

func getKeyProperty(node Node) string {
	if isTsEnumMember(node) {
		return "id"
	}
	return "key"
}

func getKey(node Node) Node { return node.Child(getKeyProperty(node)) }

func isComputedKey(node Node) bool { return !isTsEnumMember(node) && node.Bool("computed") }

var simpleNumber = regexp.MustCompile(`^(?:\d+|\d+\.\d+)$`)

// isSimpleNumber is upstream's isSimpleNumber.
func isSimpleNumber(numberString string) bool { return simpleNumber.MatchString(numberString) }

// isKeySafeToUnquote is upstream's isKeySafeToUnquote for parser "typescript".
func isKeySafeToUnquote(node Node, options *Options) bool {
	key := getKey(node)
	if !isStringLiteral(key) {
		return false
	}
	value := key.String("value")
	printed := printString(getRaw(key), options)
	if printed[1:len(printed)-1] != value {
		return false
	}
	if node.Is("TSMethodSignature") && value == "new" {
		return false
	}
	// With --strictPropertyInitialization, TypeScript treats quoted property names differently.
	if !node.Is("PropertyDefinition") && isEs5IdentifierName(value) {
		return true
	}
	// Unquoting as a number is only for the JavaScript parsers.
	return false
}

// shouldQuoteKey is upstream's shouldQuoteKey: only json, jsonc and quoteProps "consistent" quote.
func shouldQuoteKey(path *Path, options *Options) bool {
	return false
}

// shouldUnquoteKey is upstream's shouldUnquoteKey with quoteProps "as-needed".
func shouldUnquoteKey(path *Path, options *Options) bool {
	return isKeySafeToUnquote(node(path), options)
}

var leadingDigit = regexp.MustCompile(`^\d`)

// printKey is upstream's printKey.
func printKey(path *Path, options *Options, print PrintFunc) Doc {
	current := node(path)
	property := getKeyProperty(current)
	if isComputedKey(current) {
		return concat("[", print(property, nil), "]")
	}
	if shouldQuoteKey(path, options) {
		key := getKey(current)
		name := key.String("name")
		if !key.Is("Identifier") {
			name = strconv.FormatFloat(key.Get("value").(float64), 'f', -1, 64)
		}
		encoded, _ := json.Marshal(name)
		printed := printString(string(encoded), options)
		return call(path, func(path *Path) Doc { return printing.PrintComments(path, concat(printed), options, nil) }, property)
	}
	if shouldUnquoteKey(path, options) {
		value := getKey(current).String("value")
		printed := value
		if leadingDigit.MatchString(value) {
			printed = printNumber(value)
		}
		return call(path, func(path *Path) Doc { return printing.PrintComments(path, concat(printed), options, nil) }, property)
	}
	return print(property, nil)
}

// isEs5IdentifierName is the is-es5-identifier-name package upstream uses: an IdentifierName under
// ES5's grammar. ES5 draws its letters from Unicode 3.0; this uses Go's current Unicode tables, which
// can differ only for characters added since, none of which our keys use.
func isEs5IdentifierName(value string) bool {
	if value == "" {
		return false
	}
	for index, character := range value {
		isStart := character == '$' || character == '_' || unicode.IsLetter(character) || unicode.Is(unicode.Nl, character)
		if index == 0 {
			if !isStart {
				return false
			}
			continue
		}
		if !isStart && !unicode.IsDigit(character) && !unicode.Is(unicode.Mn, character) &&
			!unicode.Is(unicode.Mc, character) && !unicode.Is(unicode.Pc, character) {
			return false
		}
	}
	return true
}
