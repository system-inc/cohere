package css

// src/language-css/utilities/index.js: the predicates the printer and its print/ helpers ask of nodes and
// paths. The parser is always "css" here (SCSS and Less are out of scope), so the helpers upstream gates on
// options.parser === "scss" answer false, and say so.

import (
	"regexp"
	"slices"
	"strings"

	"github.com/system-inc/cohere/internal/format/estree"
)

var colorAdjusterFunctions = map[string]bool{
	"red":        true,
	"green":      true,
	"blue":       true,
	"alpha":      true,
	"a":          true,
	"rgb":        true,
	"hue":        true,
	"h":          true,
	"saturation": true,
	"s":          true,
	"lightness":  true,
	"l":          true,
	"whiteness":  true,
	"w":          true,
	"blackness":  true,
	"b":          true,
	"tint":       true,
	"shade":      true,
	"blend":      true,
	"blenda":     true,
	"contrast":   true,
	"hsl":        true,
	"hsla":       true,
	"hwb":        true,
	"hwba":       true,
}

// getPropOfDeclNode is upstream's function: the lowercased prop of the nearest css-decl, or "" (upstream's
// undefined, which every caller tests for truthiness; a prop is never empty).
func getPropOfDeclNode(path *astPath) string {
	declaration, found := path.FindAncestor(func(node *estree.Node) bool { return node.Type() == "css-decl" })
	if !found {
		return ""
	}
	return toLowerCase(declaration.String("prop"))
}

var wideKeywords = map[string]bool{"initial": true, "inherit": true, "unset": true, "revert": true}

func isWideKeywords(value string) bool {
	return wideKeywords[toLowerCase(value)]
}

func isKeyframeAtRuleKeywords(path *astPath, value string) bool {
	atRuleAncestorNode, found := path.FindAncestor(func(node *estree.Node) bool { return node.Type() == "css-atrule" })
	return found &&
		strings.HasSuffix(toLowerCase(atRuleAncestorNode.String("name")), "keyframes") &&
		slices.Contains([]string{"from", "to"}, toLowerCase(value))
}

func maybeToLowerCase(value string) string {
	if strings.Contains(value, "$") ||
		strings.Contains(value, "@") ||
		strings.Contains(value, "#") ||
		strings.HasPrefix(value, "%") ||
		strings.HasPrefix(value, "--") ||
		strings.HasPrefix(value, ":--") ||
		(strings.Contains(value, "(") && strings.Contains(value, ")")) {
		return value
	}
	return toLowerCase(value)
}

func insideValueFunctionNode(path *astPath, functionName string) bool {
	funcAncestorNode, found := path.FindAncestor(func(node *estree.Node) bool { return node.Type() == "value-func" })
	return found && toLowerCase(funcAncestorNode.String("value")) == functionName
}

// insideIcssRuleNode is https://github.com/css-modules/icss
func insideIcssRuleNode(path *astPath) bool {
	return path.HasAncestor(func(node *estree.Node) bool {
		if node.Type() != "css-rule" {
			return false
		}
		selector := rawString(node, "selector")
		return selector != "" && (strings.HasPrefix(selector, ":import") || strings.HasPrefix(selector, ":export"))
	})
}

// insideAtRuleNode takes upstream's atRuleNameOrAtRuleNames as a variadic list.
func insideAtRuleNode(path *astPath, atRuleNames ...string) bool {
	atRuleAncestorNode, found := path.FindAncestor(func(node *estree.Node) bool { return node.Type() == "css-atrule" })
	return found && slices.Contains(atRuleNames, toLowerCase(atRuleAncestorNode.String("name")))
}

func insideURLFunctionInImportAtRuleNode(path *astPath) bool {
	node := currentNode(path)
	groups := node.List("groups")
	if len(groups) == 0 || groups[0].String("value") != "url" || len(groups) != 2 {
		return false
	}
	atRule, found := path.FindAncestor(func(node *estree.Node) bool { return node.Type() == "css-atrule" })
	return found && atRule.String("name") == "import"
}

func isURLFunctionNode(node *estree.Node) bool {
	return node.Type() == "value-func" && toLowerCase(node.String("value")) == "url"
}

func isVarFunctionNode(node *estree.Node) bool {
	return node.Type() == "value-func" && toLowerCase(node.String("value")) == "var"
}

// detachedRulesetDeclarationPattern is /^@.+:.*$/, whose `.` is JavaScript's: anything but a line
// terminator.
var detachedRulesetDeclarationPattern = regexp.MustCompile(`^@[^\n\r\x{2028}\x{2029}]+:[^\n\r\x{2028}\x{2029}]*$`)

func isDetachedRulesetDeclarationNode(node *estree.Node) bool {
	// If a Less file ends up being parsed with the SCSS parser, Less
	// variable declarations will be parsed as atrules with names ending
	// with a colon, so keep the original case then.
	selector := node.Get("selector")
	if !estree.IsTruthy(selector) {
		return false
	}
	if text, isString := selector.(string); isString {
		return detachedRulesetDeclarationPattern.MatchString(text)
	}
	selectorNode, _ := selector.(*estree.Node)
	value := selectorNode.String("value")
	return value != "" && detachedRulesetDeclarationPattern.MatchString(value)
}

func isForKeywordNode(node *estree.Node) bool {
	return node.Type() == "value-word" && slices.Contains([]string{"from", "through", "end"}, node.String("value"))
}

func isIfElseKeywordNode(node *estree.Node) bool {
	return node.Type() == "value-word" && slices.Contains([]string{"and", "or", "not"}, node.String("value"))
}

func isEachKeywordNode(node *estree.Node) bool {
	return node.Type() == "value-word" && node.String("value") == "in"
}

func isMultiplicationNode(node *estree.Node) bool {
	return node.Type() == "value-operator" && node.String("value") == "*"
}

func isDivisionNode(node *estree.Node) bool {
	return node.Type() == "value-operator" && node.String("value") == "/"
}

func isAdditionNode(node *estree.Node) bool {
	return node.Type() == "value-operator" && node.String("value") == "+"
}

func isSubtractionNode(node *estree.Node) bool {
	return node.Type() == "value-operator" && node.String("value") == "-"
}

func isModuloNode(node *estree.Node) bool {
	return node.Type() == "value-operator" && node.String("value") == "%"
}

func isMathOperatorNode(node *estree.Node) bool {
	return isMultiplicationNode(node) ||
		isDivisionNode(node) ||
		isAdditionNode(node) ||
		isSubtractionNode(node) ||
		isModuloNode(node)
}

func isEqualityOperatorNode(node *estree.Node) bool {
	return node.Type() == "value-word" && slices.Contains([]string{"==", "!="}, node.String("value"))
}

func isRelationalOperatorNode(node *estree.Node) bool {
	return node.Type() == "value-word" && slices.Contains([]string{"<", ">", "<=", ">="}, node.String("value"))
}

// isSCSSControlDirectiveNode is gated upstream on options.parser === "scss", which is never true here.
func isSCSSControlDirectiveNode(*estree.Node) bool {
	return false
}

var detachedRulesetCallPattern = regexp.MustCompile(`^\(` + javaScriptWhitespaceClass + `*\)$`)

func isDetachedRulesetCallNode(node *estree.Node) bool {
	params := rawString(node, "params")
	return params != "" && detachedRulesetCallPattern.MatchString(params)
}

func isTemplatePlaceholderNode(node *estree.Node) bool {
	return strings.HasPrefix(node.String("name"), "prettier-placeholder")
}

func isTemplatePropNode(node *estree.Node) bool {
	return strings.HasPrefix(node.String("prop"), "@prettier-placeholder")
}

func isPostcssSimpleVarNode(currentNode *estree.Node, nextNode *estree.Node) bool {
	return currentNode.String("value") == "$$" &&
		currentNode.Type() == "value-func" &&
		nextNode.Type() == "value-word" &&
		!estree.IsTruthy(rawsOf(nextNode)["before"])
}

func hasComposesNode(node *estree.Node) bool {
	value := node.Child("value")
	return value.Type() == "value-root" &&
		value.Child("group").Type() == "value-value" &&
		toLowerCase(node.String("prop")) == "composes"
}

// hasParensAroundNode compares open and close with null, as upstream does, so an absent one (undefined)
// counts as present. Its only caller is SCSS-only, but the port keeps the comparison.
func hasParensAroundNode(node *estree.Node) bool {
	group := node.Child("value").Child("group").Child("group")
	return group.Type() == "value-paren_group" &&
		!(group.Has("open") && group.Child("open") == nil) &&
		!(group.Has("close") && group.Child("close") == nil)
}

func hasEmptyRawBefore(node *estree.Node) bool {
	before, isString := rawsOf(node)["before"].(string)
	return isString && before == ""
}

func isKeyValuePairNode(node *estree.Node) bool {
	groups := node.List("groups")
	return node.Type() == "value-comma_group" && len(groups) > 1 && groups[1].Type() == "value-colon"
}

func isKeyValuePairInParenGroupNode(node *estree.Node) bool {
	groups := node.List("groups")
	return node.Type() == "value-paren_group" && len(groups) > 0 && groups[0] != nil && isKeyValuePairNode(groups[0])
}

// isSCSSMapItemNode is gated upstream on options.parser !== "scss" returning false first, which it
// always does here.
func isSCSSMapItemNode(*astPath) bool {
	return false
}

func isInlineValueCommentNode(node *estree.Node) bool {
	return node.Type() == "value-comment" && node.Truthy("inline")
}

func isHashNode(node *estree.Node) bool {
	return node.Type() == "value-word" && node.String("value") == "#"
}

func isLeftCurlyBraceNode(node *estree.Node) bool {
	return node.Type() == "value-word" && node.String("value") == "{"
}

func isRightCurlyBraceNode(node *estree.Node) bool {
	return node.Type() == "value-word" && node.String("value") == "}"
}

func isWordNode(node *estree.Node) bool {
	return slices.Contains([]string{"value-word", "value-atword"}, node.Type())
}

func isColonNode(node *estree.Node) bool {
	return node.Type() == "value-colon"
}

func isKeyInValuePairNode(node *estree.Node, parentNode *estree.Node) bool {
	if !isKeyValuePairNode(parentNode) {
		return false
	}

	groups := parentNode.List("groups")
	index := slices.Index(groups, node)

	if index == -1 {
		return false
	}

	return index+1 < len(groups) && isColonNode(groups[index+1])
}

func isMediaAndSupportsKeywords(node *estree.Node) bool {
	value := node.String("value")
	return value != "" && slices.Contains([]string{"not", "and", "or"}, toLowerCase(value))
}

func isColorAdjusterFuncNode(node *estree.Node) bool {
	if node.Type() != "value-func" {
		return false
	}

	return colorAdjusterFunctions[toLowerCase(node.String("value"))]
}

// lastLineHasInlineComment is /\/\//.test(text.split(/[\n\r]/).pop()).
func lastLineHasInlineComment(text string) bool {
	lastLine := text[strings.LastIndexAny(text, "\n\r")+1:]
	return strings.Contains(lastLine, "//")
}

func isAtWordPlaceholderNode(node *estree.Node) bool {
	return node.Type() == "value-atword" && strings.HasPrefix(node.String("value"), "prettier-placeholder-")
}

func isConfigurationNode(node *estree.Node, parentNode *estree.Node) bool {
	if node.Child("open").String("value") != "(" ||
		node.Child("close").String("value") != ")" ||
		slices.ContainsFunc(node.List("groups"), func(group *estree.Node) bool { return group.Type() != "value-comma_group" }) {
		return false
	}
	if parentNode.Type() == "value-comma_group" {
		groups := parentNode.List("groups")
		previousIndex := slices.Index(groups, node) - 1
		if previousIndex >= 0 {
			maybeWithNode := groups[previousIndex]
			if maybeWithNode.Type() == "value-word" && maybeWithNode.String("value") == "with" {
				return true
			}
		}
	}
	return false
}

func isParenGroupNode(node *estree.Node) bool {
	return node.Type() == "value-paren_group" &&
		node.Child("open").String("value") == "(" &&
		node.Child("close").String("value") == ")"
}
