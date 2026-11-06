package javascript

// print/call-expression.js.

/*
- `NewExpression`
- `ImportExpression`
- `OptionalCallExpression`
- `CallExpression`
- `TSImportType` (TypeScript)
- `TSExternalModuleReference` (TypeScript)
*/

// printCallExpression is upstream's printCallExpression.
func printCallExpression(path *Path, options *Options, print PrintFunc) Doc {
	current := node(path)
	isNewExpression := current.Is("NewExpression")

	optional := printOptionalToken(path)
	arguments := getCallArguments(current)
	// `TSImportType.typeArguments` is after `qualifier`, not before the "arguments"
	var typeArgumentsDoc Doc = emptyDoc
	if !current.Is("TSImportType") && current.Child("typeArguments") != nil {
		typeArgumentsDoc = concatIn(path, print("typeArguments", nil), lineSuffixBoundary)
	}

	isTemplateLiteralSingleArgument := len(arguments) == 1 &&
		isTemplateOnItsOwnLine(arguments[0], originalText(options))

	if isTemplateLiteralSingleArgument ||
		// Don't break simple `import()` with long module name
		isSimpleModuleImport(path) ||
		// Dangling comments are not handled, all these special cases should have arguments #9668
		// We want to keep CommonJS- and AMD-style require calls, and AMD-style
		// define calls, as a unit.
		// e.g. `define(["some/lib"], (lib) => {`
		isCommonsJsOrAmdModuleDefinition(path) ||
		// Keep test declarations on a single line
		// e.g. `it('long name', () => {`
		isTestCall(current, parentOf(path)) {
		var printed []Doc
		iterateCallArgumentsPath(path, func(*Path, int) {
			printed = append(printed, print(nil, nil))
		})
		// Upstream skips this return when `printed[0].label?.embed`. The JavaScript printer has no embed
		// hook (printer.go), so no printed argument carries that label and the condition always holds.
		return concatIn(path, printCallee(path, print),
			optional,
			typeArgumentsDoc,
			"(",
			join(", ", printed),
			")",
		)
	}

	isDynamicImportLike := current.Is("ImportExpression", "TSImportType", "TSExternalModuleReference")

	// We detect calls on member lookups and possibly print them in a
	// special chain format. See `printMemberChain` for more info.
	if !isDynamicImportLike &&
		!isNewExpression &&
		isMemberish(current.Child("callee")) {
		names := []any{"callee"}
		if current.Child("callee").Is("ChainExpression") {
			names = append(names, "expression")
		}
		if !call(path, func(path *Path) bool { return needsParentheses(path, options) }, names...) {
			return printMemberChain(path, options, print)
		}
	}

	contents := concatIn(path, printCallee(path, print),
		optional,
		typeArgumentsDoc,
		printCallArguments(path, options, print),
	)

	// We group here when the callee is itself a call expression.
	// See `isLongCurriedCallExpression` for more info.
	if isDynamicImportLike || isCallExpression(current.Child("callee")) {
		return groupIn(path, contents)
	}

	return contents
}

// printCallee is upstream's printCallee.
func printCallee(path *Path, print PrintFunc) Doc {
	current := node(path)

	if current.Is("ImportExpression") {
		phase := ""
		if current.Truthy("phase") {
			phase = "." + current.String("phase")
		}
		return concatIn(path, "import"+phase)
	}

	if current.Is("TSImportType") {
		return concatIn(path, "import")
	}

	if current.Is("TSExternalModuleReference") {
		return concatIn(path, "require")
	}

	newKeyword := ""
	if current.Is("NewExpression") {
		newKeyword = "new "
	}
	return concatIn(path, newKeyword,
		print("callee", nil),
		lineSuffixBoundary,
	)
}

var moduleImportCallees = []string{
	"require",
	"require.resolve",
	"require.resolve.paths",
	"import.meta.resolve",
}

// isSimpleModuleImport is upstream's isSimpleModuleImport.
func isSimpleModuleImport(path *Path) bool {
	current := node(path)

	// `import("foo")`
	if !(current.Is("ImportExpression") ||
		// `type foo = import("foo")`
		current.Is("TSImportType") ||
		// `import type A = require("foo")`
		current.Is("TSExternalModuleReference") ||
		// `require("foo")`
		// `require.resolve("foo")`
		// `require.resolve.paths("foo")`
		// `import.meta.resolve("foo")`
		current.Is("CallExpression") &&
			!current.Truthy("optional") &&
			isNodeMatches(current.Child("callee"), moduleImportCallees)) {
		return false
	}

	arguments := getCallArguments(current)

	return len(arguments) == 1 && isStringLiteral(arguments[0]) && !hasAnyComment(arguments[0])
}

// isCommonsJsOrAmdModuleDefinition is upstream's isCommonsJsOrAmdModuleDefinition.
func isCommonsJsOrAmdModuleDefinition(path *Path) bool {
	current := node(path)

	if !current.Is("CallExpression") || current.Truthy("optional") {
		return false
	}

	if !current.Child("callee").Is("Identifier") {
		return false
	}

	arguments := getCallArguments(current)

	// AMD module
	if current.Child("callee").String("name") == "require" {
		return (len(arguments) == 1 && isStringLiteral(arguments[0]) || len(arguments) > 1) &&
			!hasAnyComment(arguments[0])
	}

	// CommonJS module
	if current.Child("callee").String("name") == "define" &&
		parentOf(path).Is("ExpressionStatement") {
		return len(arguments) == 1 ||
			len(arguments) == 2 && arguments[0].Is("ArrayExpression") ||
			len(arguments) == 3 &&
				isStringLiteral(arguments[0]) &&
				arguments[1].Is("ArrayExpression")
	}

	return false
}
