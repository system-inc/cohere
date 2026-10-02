package javascript

import "github.com/system-inc/cohere/internal/format/printing"

// print/expression-statement.js and semicolon/semicolon.js. The Vue event binding, HTML inline event
// handler and markdown-embedded JSX paths answer false here: those hosts never run this printer.

// shouldPrintSemicolon is upstream's shouldPrintSemicolon.
func shouldPrintSemicolon(path *Path, options *Options) bool {
	return settingsOf(options).Semi
}

// printExpressionStatement is upstream's printExpressionStatement.
func printExpressionStatement(path *Path, options *Options, print PrintFunc) Doc {
	parts := []any{print("expression", nil)}

	if shouldExpressionStatementPrintLeadingSemicolon(path, options) {
		if shouldExpressionStatementPrintOwnComments(path, options) {
			leading := getComments(node(path), commentLeading, nil)
			var typeCastComment Node
			if len(leading) > 0 {
				typeCastComment = leading[len(leading)-1]
			}
			typeCastCommentDoc := printing.PrintLeadingComments(path, options, func(comment Node) bool {
				return comment == typeCastComment
			})
			return printing.PrintComments(path, concat(append([]any{";", typeCastCommentDoc}, parts...)...), options,
				func(comment Node) bool { return comment != typeCastComment })
		}
		parts = append([]any{";"}, parts...)
	} else if shouldPrintSemicolon(path, options) {
		parts = append(parts, ";")
	}
	return concat(parts...)
}

// shouldExpressionStatementPrintLeadingSemicolon is upstream's function of that name.
func shouldExpressionStatementPrintLeadingSemicolon(path *Path, options *Options) bool {
	if settingsOf(options).Semi {
		return false
	}
	current := node(path)
	if !current.Is("ExpressionStatement") {
		return false
	}
	key := keyOf(path)
	parent := parentOf(path)
	if (key == "body" && parent.Is("Program", "BlockStatement", "StaticBlock", "TSModuleBlock") ||
		key == "consequent" && parent.Is("SwitchCase")) &&
		call(path, func(path *Path) bool { return expressionNeedsAsiProtection(path, options) }, "expression") {
		return true
	}
	return false
}

// expressionNeedsAsiProtection is upstream's expressionNeedsAsiProtection.
func expressionNeedsAsiProtection(path *Path, options *Options) bool {
	current := node(path)
	switch current.Type() {
	case "ParenthesizedExpression", "TypeCastExpression", "TSTypeAssertion", "ArrayExpression", "ArrayPattern",
		"TemplateLiteral", "TemplateElement", "RegExpLiteral":
		return true
	case "ArrowFunctionExpression":
		if !shouldPrintParamsWithoutParens(path, options) {
			return true
		}
	case "UnaryExpression":
		operator := current.String("operator")
		if current.Bool("prefix") && (operator == "+" || operator == "-") {
			return true
		}
	case "BindExpression":
		if current.Child("object") == nil {
			return true
		}
	case "Literal":
		if current.Get("regex") != nil {
			return true
		}
	default:
		if isJsxElement(current) {
			return true
		}
	}

	if needsParentheses(path, options) {
		return true
	}
	if !hasNakedLeftSide(current) {
		return false
	}
	return call(path, func(path *Path) bool { return expressionNeedsAsiProtection(path, options) },
		getLeftSidePathName(current)...)
}
