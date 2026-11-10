package javascript

import (
	"slices"

	"github.com/system-inc/cohere/internal/format/doc"
)

// print/call-arguments.js.

/*
- `NewExpression`
- `ImportExpression`
- `OptionalCallExpression`
- `CallExpression`
- `TSImportType` (TypeScript)
- `TSExternalModuleReference` (TypeScript)
*/

// printCallArguments is upstream's printCallArguments.
func printCallArguments(path *Path, options *Options, print PrintFunction) Doc {
	current := node(path)

	arguments := getCallArguments(current)

	if len(arguments) == 0 {
		return groupIn(path, concatIn(
			path,
			doc.Text("("),
			printDanglingCommentsInList(path, options, nil),
			doc.Text(")"),
		))
	}

	lastArgIndex := len(arguments) - 1

	// useEffect(() => { ... }, [foo, bar, baz])
	// useImperativeHandle(ref, () => { ... }, [foo, bar, baz])
	if isReactHookCallWithDepsArray(arguments) {
		parts := []Doc{doc.Text("(")}
		iterateCallArgumentsPath(path, func(path *Path, index int) {
			parts = append(parts, print("", nil))
			if index != lastArgIndex {
				parts = append(parts, doc.Text(", "))
			}
		})
		parts = append(parts, doc.Text(")"))
		return concatIn(path, parts...)
	}

	anyArgEmptyLine := false
	var printedArguments []Doc
	iterateCallArgumentsPath(path, func(path *Path, index int) {
		argument := node(path)
		argumentDoc := print("", nil)

		if index == lastArgIndex {
			// do nothing
		} else if isNextLineEmptyAfter(argument, options) {
			anyArgEmptyLine = true
			argumentDoc = concatIn(path, argumentDoc, doc.Text(","), hardline, hardline)
		} else {
			argumentDoc = concatIn(path, argumentDoc, doc.Text(","), line)
		}

		printedArguments = append(printedArguments, argumentDoc)
	})

	// Upstream also requires `path.root.type !== "NGRoot"` (Angular does not allow trailing comma);
	// the root here is always a TypeScript Program, so that test always holds.
	var trailingComma Doc = emptyDoc
	// Dynamic imports cannot have trailing commas
	if !current.Is("ImportExpression") &&
		!current.Is("TSImportType") &&
		!current.Is("TSExternalModuleReference") {
		trailingComma = printTrailingComma(options, "all")
	}

	allArgsBrokenOut := func() Doc {
		return groupWithIn(path, concatIn(
			path,
			doc.Text("("),
			indentIn(path, concatIn(path, slices.Concat([]Doc{line}, printedArguments)...)),
			trailingComma,
			line,
			doc.Text(")"),
		),
			doc.GroupOptions{ShouldBreak: true},
		)
	}

	if anyArgEmptyLine ||
		(!parentOf(path).Is("Decorator") && isFunctionCompositionArguments(arguments)) {
		return allArgsBrokenOut()
	}

	if shouldExpandFirstArgument(arguments) {
		tailArguments := printedArguments[1:]
		for _, tailArgument := range tailArguments {
			if doc.WillBreak(tailArgument) {
				return allArgsBrokenOut()
			}
		}
		firstArgument, bailedOut := printCatchingArgExpansionBailout(
			path,
			print,
			getCallArgumentSelector(current, 0),
			&printArguments{expandFirstArgument: true},
		)
		if bailedOut {
			return allArgsBrokenOut()
		}

		if doc.WillBreak(firstArgument) {
			return concatIn(path, breakParent,
				conditionalGroup([]Doc{
					concatIn(path, slices.Concat(
						[]Doc{
							doc.Text("("),
							groupWithIn(path, firstArgument, doc.GroupOptions{ShouldBreak: true}),
							doc.Text(", "),
						},
						tailArguments,
						[]Doc{doc.Text(")")},
					)...),
					allArgsBrokenOut(),
				}, doc.GroupOptions{}),
			)
		}

		return conditionalGroup([]Doc{
			concatIn(path, slices.Concat(
				[]Doc{doc.Text("("), firstArgument, doc.Text(", ")},
				tailArguments,
				[]Doc{doc.Text(")")},
			)...),
			concatIn(path, slices.Concat(
				[]Doc{
					doc.Text("("),
					groupWithIn(path, firstArgument, doc.GroupOptions{ShouldBreak: true}),
					doc.Text(", "),
				},
				tailArguments,
				[]Doc{doc.Text(")")},
			)...),
			allArgsBrokenOut(),
		}, doc.GroupOptions{})
	}

	if shouldExpandLastArgument(arguments, printedArguments, options) {
		headArguments := printedArguments[:len(printedArguments)-1]
		for _, headArgument := range headArguments {
			if doc.WillBreak(headArgument) {
				return allArgsBrokenOut()
			}
		}
		lastArgument, bailedOut := printCatchingArgExpansionBailout(
			path,
			print,
			getCallArgumentSelector(current, -1),
			&printArguments{expandLastArgument: true},
		)
		if bailedOut {
			return allArgsBrokenOut()
		}

		if doc.WillBreak(lastArgument) {
			return concatIn(path, breakParent,
				conditionalGroup([]Doc{
					concatIn(path, slices.Concat(
						[]Doc{doc.Text("(")},
						headArguments,
						[]Doc{groupWithIn(path, lastArgument, doc.GroupOptions{ShouldBreak: true}), doc.Text(")")},
					)...),
					allArgsBrokenOut(),
				}, doc.GroupOptions{}),
			)
		}

		return conditionalGroup([]Doc{
			concatIn(path, slices.Concat([]Doc{doc.Text("(")}, headArguments, []Doc{lastArgument, doc.Text(")")})...),
			concatIn(path, slices.Concat(
				[]Doc{doc.Text("(")},
				headArguments,
				[]Doc{groupWithIn(path, lastArgument, doc.GroupOptions{ShouldBreak: true}), doc.Text(")")},
			)...),
			allArgsBrokenOut(),
		}, doc.GroupOptions{})
	}

	contents := concatIn(path, doc.Text("("),
		indentIn(path, concatIn(path, slices.Concat([]Doc{softline}, printedArguments)...)),
		trailingComma,
		softline,
		doc.Text(")"),
	)
	if isLongCurriedCallExpression(path) {
		// By not wrapping the arguments in a group, the printer prioritizes
		// breaking up these arguments rather than the args of the parent call.
		return contents
	}

	shouldBreak := anyArgEmptyLine
	for _, printedArgument := range printedArguments {
		if doc.WillBreak(printedArgument) {
			shouldBreak = true
			break
		}
	}
	return groupWithIn(path, contents, doc.GroupOptions{ShouldBreak: shouldBreak})
}

// printCatchingArgExpansionBailout is upstream's
//
//	try { printed = print(selector, args) } catch (caught) { if (caught instanceof ArgExpansionBailout) ... }
//
// as a function: Go has no try/catch, so the deferred recover takes the place of the catch. The
// second result is true where upstream caught ArgExpansionBailout; any other panic is re-raised, as
// upstream rethrows. The selector ends in an index for a call's argument, so it is printed through
// callSelector, the Call that mainPrint runs for an array selector.
func printCatchingArgExpansionBailout(
	path *Path,
	print PrintFunction,
	selector pathSelector,
	arguments *printArguments,
) (printed Doc, bailedOut bool) {
	defer func() {
		if caught := recover(); caught != nil {
			if _, isBailout := caught.(argumentExpansionBailout); isBailout {
				printed, bailedOut = nil, true
				return
			}
			/* c8 ignore next */
			panic(caught)
		}
	}()
	return callSelector(path, func(*Path) Doc { return print("", arguments) }, selector), false
}

// couldExpandArgument is upstream's couldExpandArgument. Upstream's arrowChainRecursion defaults to false; Go
// has no default parameters, so callers pass it.
func couldExpandArgument(argument Node, arrowChainRecursion bool) bool {
	if isObjectExpression(argument) &&
		(len(argument.List("properties")) > 0 || hasAnyComment(argument)) {
		return true
	}

	if isArrayExpression(argument) && (len(argument.List("elements")) > 0 || hasAnyComment(argument)) {
		return true
	}

	if (isBinaryCastExpression(argument) || argument.Is("TSTypeAssertion")) &&
		couldExpandArgument(argument.Child("expression"), false) {
		return true
	}

	if argument.Is("FunctionExpression") ||
		argument.Is("DoExpression") ||
		argument.Is("ModuleExpression") {
		return true
	}

	if argument.Is("ArrowFunctionExpression") {
		body := argument.Child("body")

		if body.Is("BlockStatement") ||
			isJsxElement(body) ||
			isObjectExpression(body) ||
			isArrayExpression(body) {
			return true
		}

		if body.Is("ArrowFunctionExpression") && couldExpandArgument(body, true) {
			return true
		}

		if !arrowChainRecursion {
			if body.Is("ConditionalExpression") {
				return true
			}

			if isCallExpression(stripChainElementWrappers(body)) {
				return true
			}
		}
	}

	return false
}

// shouldExpandLastArgument is upstream's shouldExpandLastArgument.
func shouldExpandLastArgument(arguments []Node, argumentDocs []Doc, options *Options) bool {
	// Upstream first returns true for a lone argument whose doc carries `label.embed` (and not
	// `label.hug === false`). The JavaScript printer has no embed hook (printer.go), so no argument doc
	// carries that label and the branch is never taken; argDocs is kept for upstream's signature.

	lastArgument := arguments[len(arguments)-1]
	var penultimateArgument Node
	if len(arguments) >= 2 {
		penultimateArgument = arguments[len(arguments)-2]
	}
	return !hasComment(lastArgument, commentLeading, nil) &&
		!hasComment(lastArgument, commentTrailing, nil) &&
		couldExpandArgument(lastArgument, false) &&
		// If the last two arguments are of the same type,
		// disable last element expansion.
		(penultimateArgument == nil || penultimateArgument.Type() != lastArgument.Type()) &&
		// useMemo(() => func(), [foo, bar, baz])
		(len(arguments) != 2 ||
			!penultimateArgument.Is("ArrowFunctionExpression") ||
			!isArrayExpression(lastArgument)) &&
		!(len(arguments) > 1 && isConciselyPrintedArray(lastArgument, options))
}

// shouldExpandFirstArgument is upstream's shouldExpandFirstArgument.
func shouldExpandFirstArgument(arguments []Node) bool {
	if len(arguments) != 2 {
		return false
	}

	firstArgument, secondArgument := arguments[0], arguments[1]

	if firstArgument.Is("ModuleExpression") &&
		isTypeModuleObjectExpression(secondArgument) {
		return true
	}

	return !hasAnyComment(firstArgument) &&
		(firstArgument.Is("FunctionExpression") ||
			(firstArgument.Is("ArrowFunctionExpression") &&
				firstArgument.Child("body").Is("BlockStatement"))) &&
		!secondArgument.Is("FunctionExpression") &&
		!secondArgument.Is("ArrowFunctionExpression") &&
		!secondArgument.Is("ConditionalExpression") &&
		isHopefullyShortCallArgument(secondArgument) &&
		!couldExpandArgument(secondArgument, false)
}

// isHopefullyShortCallArgument is upstream's isHopefullyShortCallArgument.
//
// A hack to fix most manifestations of
// https://github.com/prettier/prettier/issues/2456
// https://github.com/prettier/prettier/issues/5172
// https://github.com/prettier/prettier/issues/12892
// A proper (printWidth-aware) fix for those would require a complex change in the doc printer.
func isHopefullyShortCallArgument(node Node) bool {
	if node.Is("ParenthesizedExpression") {
		return isHopefullyShortCallArgument(node.Child("expression"))
	}

	if isBinaryCastExpression(node) || node.Is("TypeCastExpression") {
		typeAnnotation := node.Child("typeAnnotation")
		if typeAnnotation.Is("TypeAnnotation") {
			typeAnnotation = typeAnnotation.Child("typeAnnotation")
		}

		if typeAnnotation.Is("TSArrayType") {
			typeAnnotation = typeAnnotation.Child("elementType")
			if typeAnnotation.Is("TSArrayType") {
				typeAnnotation = typeAnnotation.Child("elementType")
			}
		}

		if typeAnnotation.Is("GenericTypeAnnotation") ||
			typeAnnotation.Is("TSTypeReference") {
			typeArguments := typeAnnotation.Child("typeArguments")
			if typeAnnotation.Is("GenericTypeAnnotation") {
				typeArguments = typeAnnotation.Child("typeParameters")
			}
			if typeArguments != nil && len(typeArguments.List("params")) == 1 {
				typeAnnotation = typeArguments.List("params")[0]
			}
		}
		return isSimpleType(typeAnnotation) && isSimpleCallArgument(node.Child("expression"), 1)
	}

	if isCallLikeExpression(node) && len(getCallArguments(node)) > 1 {
		return false
	}

	if isBinaryish(node) {
		return isSimpleCallArgument(node.Child("left"), 1) && isSimpleCallArgument(node.Child("right"), 1)
	}

	return isRegExpLiteral(node) || isSimpleCallArgument(node, 2)
}

// isReactHookCallWithDepsArray is upstream's isReactHookCallWithDepsArray.
//
// Checks if the arguments of a function are a call to a React Hook with a dependencies array.
func isReactHookCallWithDepsArray(arguments []Node) bool {
	if len(arguments) == 2 {
		/**
		 * useEffect(() => {
		 *   // do something
		 * }, [dep1, dep2, dep2])
		 */
		return isValidHookCallbackAndDepsFormat(arguments /* baseIndex */, 0)
	}
	if len(arguments) == 3 {
		/**
		 * useImperativeHandle(ref, () => {
		 *   // do something
		 * }, [dep1, dep2, dep2]);
		 */
		return arguments[0].Is("Identifier") &&
			isValidHookCallbackAndDepsFormat(arguments /* baseIndex */, 1)
	}
	return false
}

// isValidHookCallbackAndDepsFormat is upstream's isValidHookCallbackAndDepsFormat.
func isValidHookCallbackAndDepsFormat(arguments []Node, baseIndex int) bool {
	maybeArrowFunction := arguments[baseIndex]
	maybeDepsArray := arguments[baseIndex+1]
	if !(maybeArrowFunction.Is("ArrowFunctionExpression") &&
		len(getFunctionParameters(maybeArrowFunction)) == 0 &&
		maybeArrowFunction.Child("body").Is("BlockStatement") &&
		maybeDepsArray.Is("ArrayExpression")) {
		return false
	}
	for _, argument := range arguments {
		if hasAnyComment(argument) {
			return false
		}
	}
	return true
}

// isTypeModuleObjectExpression is upstream's isTypeModuleObjectExpression.
//
// `{ type: "module" }` and `{"type": "module"}`
func isTypeModuleObjectExpression(node Node) bool {
	if !(node.Is("ObjectExpression") && len(node.List("properties")) == 1) {
		return false
	}

	property := node.List("properties")[0]

	if !isObjectProperty(property) {
		return false
	}

	return !property.Truthy("computed") &&
		((property.Child("key").Is("Identifier") && property.Child("key").String("name") == "type") ||
			(isStringLiteral(property.Child("key")) && property.Child("key").String("value") == "type")) &&
		isStringLiteral(property.Child("value")) &&
		property.Child("value").String("value") == "module"
}
