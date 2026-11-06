package javascript

import (
	"strings"

	"github.com/system-inc/cohere/internal/format/doc"
	"github.com/system-inc/cohere/internal/format/estree"
	"github.com/system-inc/cohere/internal/format/formatoptions"
)

// utilities/call-arguments.js, utilities/call-or-new-expression-parentheses.js,
// utilities/is-function-composition-arguments.js, utilities/is-simple-call-argument.js,
// utilities/is-lone-short-argument.js, utilities/is-long-curried-call-expression.js,
// utilities/is-iife-callee-or-tagged-template-expression-tag.js, utilities/is-boolean-type-coercion.js.

// getCallArguments is upstream's getCallArguments. Upstream memoizes it in a WeakMap; no caller
// mutates the array or compares it by identity, and a cache on settings would need options, which
// upstream's callers (canAttachComment, isTestCall, isSimpleCallArgument) do not have, so the Go
// computes it each time and keeps upstream's one-argument signature.
func getCallArguments(node Node) []Node {
	var arguments []Node
	if node.Is("ImportExpression", "TSImportType") {
		arguments = []Node{node.Child("source")}

		if node.Truthy("options") {
			arguments = append(arguments, node.Child("options"))
		}
	} else if node.Is("TSExternalModuleReference") {
		arguments = []Node{node.Child("expression")}
	} else {
		arguments = node.List("arguments")
	}

	return arguments
}

// iterateCallArgumentsPath is upstream's iterateCallArgumentsPath.
func iterateCallArgumentsPath(path *Path, iteratee func(path *Path, index int)) {
	node := node(path)

	if node.Is("ImportExpression", "TSImportType") {
		call(path, func(path *Path) any { iteratee(path, 0); return nil }, "source")

		if node.Truthy("options") {
			call(path, func(path *Path) any { iteratee(path, 1); return nil }, "options")
		}
	} else if node.Is("TSExternalModuleReference") {
		call(path, func(path *Path) any { iteratee(path, 0); return nil }, "expression")
	} else {
		each(path, iteratee, "arguments")
	}
}

// getCallArgumentSelector is upstream's getCallArgumentSelector: the print selector of the argument
// at index, negative from the end. Upstream's RangeError is a panic.
func getCallArgumentSelector(node Node, index int) pathSelector {
	if node.Is("ImportExpression", "TSImportType") {
		lastIndex := -1
		if node.Truthy("options") {
			lastIndex = -2
		}
		if index == 0 || index == lastIndex {
			return pathSelector{property: "source"}
		}

		if node.Truthy("options") && (index == 1 || index == -1) {
			return pathSelector{property: "options"}
		}

		// Invariant: printCallArguments, the only caller, asks for index 0 or -1, both answered above.
		panic("javascript: Invalid argument index")
	}

	if node.Is("TSExternalModuleReference") {
		if index == 0 || index == -1 {
			return pathSelector{property: "expression"}
		}
	} else {
		argumentIndex := index
		if argumentIndex < 0 {
			argumentIndex = len(node.List("arguments")) + index
		}
		if argumentIndex >= 0 && argumentIndex < len(node.List("arguments")) {
			return pathSelector{property: "arguments", index: argumentIndex, endsInIndex: true}
		}
	}

	// Invariant: printCallArguments, the only caller, asks for index 0 or -1 of a call with at least one
	// argument, which the branches above answer.
	panic("javascript: Invalid argument index")
}

// getCallOrNewExpressionClosingParenthesisIndex is upstream's function of that name. The second
// result is false where upstream returns undefined; the fork runs in production, where upstream
// returns rather than throws.
func getCallOrNewExpressionClosingParenthesisIndex(callOrNewExpression Node, options *Options) (int, bool) {
	closingParenthesisIndex := locEnd(callOrNewExpression) - 1

	if closingParenthesisIndex < 0 || closingParenthesisIndex >= len(options.OriginalText) ||
		options.OriginalText[closingParenthesisIndex] != ')' {
		return 0, false
	}

	return closingParenthesisIndex, true
}

// getCallOrNewExpressionOpeningParenthesisIndex is upstream's function of that name.
func getCallOrNewExpressionOpeningParenthesisIndex(callOrNewExpression Node, options *Options) (int, bool) {
	_, hasClosingParenthesis := getCallOrNewExpressionClosingParenthesisIndex(callOrNewExpression, options)

	if !hasClosingParenthesis {
		return 0, false
	}

	text := stripComments(options)
	typeArgumentsOrCallee := callOrNewExpression.Child("typeArguments")
	if typeArgumentsOrCallee == nil {
		typeArgumentsOrCallee = callOrNewExpression.Child("callee")
	}
	start := locEnd(typeArgumentsOrCallee)
	openingParenthesisIndex := -1
	if start <= len(text) {
		if found := strings.IndexByte(text[start:], '('); found != -1 {
			openingParenthesisIndex = start + found
		}
	}

	if openingParenthesisIndex == -1 {
		return 0, false
	}

	return openingParenthesisIndex, true
}

// isInsideCallOrNewExpressionParentheses is upstream's isInsideCallOrNewExpressionParentheses.
func isInsideCallOrNewExpressionParentheses(callOrNewExpression Node, nodeOrComment Node, options *Options) bool {
	closingParenthesisIndex, hasClosingParenthesis := getCallOrNewExpressionClosingParenthesisIndex(
		callOrNewExpression,
		options,
	)

	if !hasClosingParenthesis {
		return false
	}

	if locEnd(nodeOrComment) > closingParenthesisIndex {
		return false
	}

	openingParenthesisIndex, hasOpeningParenthesis := getCallOrNewExpressionOpeningParenthesisIndex(
		callOrNewExpression,
		options,
	)

	if !hasOpeningParenthesis {
		return false
	}

	return locStart(nodeOrComment) > openingParenthesisIndex
}

// isFunctionCompositionArguments is upstream's isFunctionCompositionArguments.
//
// Logic to check for args with multiple anonymous functions. For instance,
// the following call should be split on multiple lines for readability:
// source.pipe(map((x) => x + x), filter((x) => x % 2 === 0))
func isFunctionCompositionArguments(arguments []Node) bool {
	if len(arguments) <= 1 {
		return false
	}
	count := 0
	for _, argument := range arguments {
		if isFunctionOrArrowExpression(argument) {
			count += 1
			if count > 1 {
				return true
			}
		} else {
			argument = stripChainElementWrappers(argument)
			if isCallExpression(argument) {
				for _, childArgument := range getCallArguments(argument) {
					if isFunctionOrArrowExpression(childArgument) {
						return true
					}
				}
			}
		}
	}
	return false
}

var simpleCallArgumentUnaryOperators = map[string]bool{"!": true, "-": true, "+": true, "~": true}

func isSingleWordType(node Node) bool {
	return node.Is("Identifier", "ThisExpression", "Super", "PrivateName", "PrivateIdentifier")
}

// isSimpleCallArgument is upstream's isSimpleCallArgument. Upstream's default depth is 2; Go has no
// default parameters, so callers pass it.
func isSimpleCallArgument(node Node, depth int) bool {
	if depth <= 0 {
		return false
	}

	isChildSimple := func(child Node) bool { return isSimpleCallArgument(child, depth-1) }

	unwrapped := stripChainElementWrappers(node)

	if isRegExpLiteral(unwrapped) {
		return doc.StringWidth(regExpPattern(unwrapped)) <= 5
	}

	if isLiteral(unwrapped) ||
		isSingleWordType(unwrapped) ||
		unwrapped.Is("ArgumentPlaceholder") {
		return true
	}

	if unwrapped.Is("TemplateLiteral") {
		for _, element := range unwrapped.List("quasis") {
			if strings.Contains(templateElementRaw(element), "\n") {
				return false
			}
		}
		for _, expression := range unwrapped.List("expressions") {
			if !isChildSimple(expression) {
				return false
			}
		}
		return true
	}

	if isObjectExpression(unwrapped) {
		for _, property := range unwrapped.List("properties") {
			if !(!property.Truthy("computed") &&
				(property.Truthy("shorthand") ||
					property.Child("value") != nil && isChildSimple(property.Child("value")))) {
				return false
			}
		}
		return true
	}

	if isArrayExpression(unwrapped) {
		for _, element := range unwrapped.List("elements") {
			if !(element == nil || isChildSimple(element)) {
				return false
			}
		}
		return true
	}

	if isCallLikeExpression(unwrapped) {
		if unwrapped.Is("ImportExpression") ||
			isSimpleCallArgument(unwrapped.Child("callee"), depth) {
			arguments := getCallArguments(unwrapped)
			if len(arguments) > depth {
				return false
			}
			for _, argument := range arguments {
				if !isChildSimple(argument) {
					return false
				}
			}
			return true
		}
		return false
	}

	if isMemberExpression(unwrapped) {
		return isSimpleCallArgument(unwrapped.Child("object"), depth) &&
			isSimpleCallArgument(unwrapped.Child("property"), depth)
	}

	if unwrapped.Is("UnaryExpression") &&
		simpleCallArgumentUnaryOperators[unwrapped.String("operator")] ||
		unwrapped.Is("UpdateExpression") {
		return isSimpleCallArgument(unwrapped.Child("argument"), depth)
	}

	return false
}

// regExpPattern is upstream's `node.pattern ?? node.regex.pattern` on a regular expression literal.
func regExpPattern(node Node) string {
	if node.Get("pattern") != nil {
		return node.String("pattern")
	}
	if regex, isRegex := node.Get("regex").(*estree.Regex); isRegex && regex != nil {
		return regex.Pattern
	}
	return ""
}

const loneShortArgumentThresholdRate = 0.25

// isLoneShortArgument is upstream's isLoneShortArgument. Upstream compares `.length`, UTF-16 code
// units, against the threshold, so the Go does too (utf16Length) rather than measuring width.
func isLoneShortArgument(node Node, options *Options) bool {
	printWidth := settingsOf(options).PrintWidth

	if hasAnyComment(node) {
		return false
	}

	threshold := float64(printWidth) * loneShortArgumentThresholdRate

	if node.Is("ThisExpression") ||
		node.Is("Identifier") && float64(utf16Length(node.String("name"))) <= threshold ||
		isSignedNumericLiteral(node) && !hasAnyComment(node.Child("argument")) {
		return true
	}

	regexpPattern := ""
	if node.Is("Literal") && node.Has("regex") {
		regexpPattern = regExpPattern(node)
	}
	if regexpPattern == "" && node.Is("RegExpLiteral") {
		regexpPattern = node.String("pattern")
	}

	if regexpPattern != "" {
		return float64(utf16Length(regexpPattern)) <= threshold
	}

	if isStringLiteral(node) {
		return float64(utf16Length(printString(getRaw(node), options))) <= threshold
	}

	if node.Is("TemplateLiteral") {
		quasis := node.List("quasis")
		return len(node.List("expressions")) == 0 &&
			float64(utf16Length(templateElementRaw(quasis[0]))) <= threshold &&
			!strings.Contains(templateElementRaw(quasis[0]), "\n")
	}

	if node.Is("UnaryExpression") {
		// Upstream passes `{ printWidth }` alone, so a string argument goes through printString with
		// singleQuote unset, preferring double quotes. The zero settings reproduce that object.
		return isLoneShortArgument(node.Child("argument"), &Options{
			Settings: &settings{Options: formatoptions.Options{PrintWidth: printWidth}},
		})
	}

	if node.Is("CallExpression") &&
		len(node.List("arguments")) == 0 &&
		node.Child("callee").Is("Identifier") {
		return float64(utf16Length(node.Child("callee").String("name"))) <= threshold-2
	}

	return isLiteral(node)
}

// isLongCurriedCallExpression is upstream's isLongCurriedCallExpression.
//
// Logic to determine if a call is a “long curried function call”.
// See https://github.com/prettier/prettier/issues/1420.
//
// `connect(a, b, c)(d)`
// In the above call expression, the second call is the parent node and the
// first call is the current node.
func isLongCurriedCallExpression(path *Path) bool {
	node, parent, key := node(path), parentOf(path), keyOf(path)
	return key == "callee" &&
		isCallExpression(node) &&
		isCallExpression(parent) &&
		len(parent.List("arguments")) > 0 &&
		len(node.List("arguments")) > len(parent.List("arguments"))
}

// isIifeCalleeOrTaggedTemplateExpressionTag is upstream's isIifeCalleeOrTaggedTemplateExpressionTag.
func isIifeCalleeOrTaggedTemplateExpressionTag(path *Path) bool {
	node := node(path)
	return node.Is("FunctionExpression", "ArrowFunctionExpression") &&
		(keyOf(path) == "callee" && isCallExpression(parentOf(path)) ||
			keyOf(path) == "tag" && parentOf(path).Is("TaggedTemplateExpression"))
}

// isBooleanTypeCoercion is upstream's isBooleanTypeCoercion.
func isBooleanTypeCoercion(node Node) bool {
	return node.Is("CallExpression") &&
		!node.Truthy("optional") &&
		len(node.List("arguments")) == 1 &&
		node.Child("callee").Is("Identifier") &&
		node.Child("callee").String("name") == "Boolean"
}
