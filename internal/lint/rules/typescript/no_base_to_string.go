package typescript

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/system-inc/cohere/internal/lint/checking"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// noBaseToStringUsefulness is upstream's `Usefulness` enum.
//
// The three values are a lattice rather than a flag, and the string spellings are load bearing: they
// are interpolated straight into the message, so `Sometimes` renders as "may" and `Never` as "will".
type noBaseToStringUsefulness string

const (
	// noBaseToStringAlways means the value stringifies usefully, so nothing is reported.
	noBaseToStringAlways noBaseToStringUsefulness = "always"
	// noBaseToStringNever means it definitely does not.
	noBaseToStringNever noBaseToStringUsefulness = "will"
	// noBaseToStringSometimes means some constituent does and some does not.
	noBaseToStringSometimes noBaseToStringUsefulness = "may"
)

// NoBaseToStringOptions is the rule's option surface.
type NoBaseToStringOptions struct {
	// CheckUnknown extends the rule to `unknown` and to an unconstrained type parameter.
	CheckUnknown bool
	// IgnoredTypeNames are stringified type names to leave alone.
	//
	// This is the one option in this family whose default is not a zero value, which makes the
	// decoder load bearing rather than defensive: a decoder returning an empty slice for an absent
	// key would silently start reporting on Error, RegExp, URL and URLSearchParams.
	IgnoredTypeNames []string
}

// DefaultNoBaseToStringSettings is upstream's `defaultOptions`.
func DefaultNoBaseToStringSettings() NoBaseToStringOptions {
	return NoBaseToStringOptions{
		CheckUnknown:     false,
		IgnoredTypeNames: []string{"Error", "RegExp", "URL", "URLSearchParams"},
	}
}

// noBaseToStringRawOptions is the wire shape, with pointers so an absent key stays distinguishable
// from an explicit empty one.
type noBaseToStringRawOptions struct {
	CheckUnknown     *bool     `json:"checkUnknown"`
	IgnoredTypeNames *[]string `json:"ignoredTypeNames"`
}

// DecodeNoBaseToStringOptions reads the rule's configuration.
//
// cohere's config layer strips ESLint's `[severity, options]` tuple before dispatch, so what arrives
// is the bare object rather than upstream's one-element array.
//
// The pointer on IgnoredTypeNames is what separates an absent key from an explicit empty list, and
// the two mean opposite things here: absent keeps the four builtin names, explicit-empty clears them
// and asks the rule to report on an `Error`. Upstream expresses the same distinction through
// `option.ignoredTypeNames ?? []` over a defaults merge.
func DecodeNoBaseToStringOptions(raw []byte) (NoBaseToStringOptions, error) {
	decoded, decodeOptionsError := rule.DecodeOptionsInto[noBaseToStringRawOptions]()(raw)
	if decodeOptionsError != nil {
		return DefaultNoBaseToStringSettings(), decodeOptionsError
	}

	options := DefaultNoBaseToStringSettings()
	if decoded.CheckUnknown != nil {
		options.CheckUnknown = *decoded.CheckUnknown
	}
	if decoded.IgnoredTypeNames != nil {
		options.IgnoredTypeNames = *decoded.IgnoredTypeNames
	}
	return options, nil
}

// NoBaseToString flags a value being stringified that has no useful string form.
//
//	valid:   String(new Date());               Date declares its own toString
//	valid:   `${'literal'}`;
//	valid:   [1, 2].join(', ');
//	invalid: String({});                       renders as [object Object]
//	invalid: `${{}}`;
//	invalid: '' + {};
//	invalid: [{}, {}].join(', ');
//
// `[object Object]` in a log line or a user-facing string is one of the quietest defects available:
// nothing throws, the type system is satisfied, and the information is simply gone. The rule asks
// whether a value being coerced to a string has anything to say.
//
// # The judgment is a three-valued recursion over the type, not over the syntax
//
// A value stringifies usefully Always, Never, or Sometimes, and the third is what a union produces
// when its constituents disagree. That value is interpolated into the message, so the certainty is
// something the reader sees: "will use Object's default" against "may use Object's default".
//
// The recursion walks unions, intersections, tuple elements, array element types, and a type
// parameter's constraint, with a visited set so a self-referential array terminates. Two combining
// rules, both upstream's and both asymmetric on purpose:
//
//	union          all Never is Never, all Always is Always, anything else is Sometimes
//	intersection   ANY Always is Always, otherwise Never
//
// The intersection rule is the one that reads oddly and is right: intersecting a useless shape with
// a useful one gives a value that has the useful `toString`, so the whole is fine.
//
// # The base question is where a coercion method is declared
//
// Upstream asks for `toLocaleString`, `toString` and `valueOf` in that order, and for each looks at
// every declaration. A declaration whose parent is anything other than the `Object` interface means
// somebody wrote their own, so the type is useful. Only when all the ones that exist come from
// `Object` itself is the value useless. Measured against our checker: `{}` finds all three on
// `Object`, `string` finds `toString` on `String`, and a class with its own method finds it on a
// class declaration, which is exactly the discrimination upstream needs.
//
// An explicit `[Symbol.toPrimitive]` short-circuits that entirely, because such a declaration is by
// construction user-defined.
//
// # `unknown` is a third state and the option is what reads it
//
// The base question answers yes, no, or "no coercion method at all", and that last case covers both
// `any` and `unknown`. Upstream distinguishes them by flags, and only reports on `unknown` when
// `checkUnknown` is set. An unconstrained type parameter is treated as `unknown` for the same reason.
//
// # This rule reads the program
//
// `IsSymbolFromDefaultLibrary` walks the program's own default-library files to decide whether the
// `Symbol` in `[Symbol.toPrimitive]` is the builtin one, which is a read outside the file being
// linted.
//
// # Cost
//
// Five anchors, and the recursion is bounded by the type's own depth with a visited set. Every anchor
// is a coercion site rather than an arbitrary expression, so the rule declines almost everything in a
// real file before asking a type question.
var NoBaseToString = rule.Rule[NoBaseToStringOptions]{
	Name: "@typescript-eslint/no-base-to-string",

	// Every finding is decided by walking a type, so there is no syntactic subset of this rule.
	NeedsTypeChecker: true,
	TypeReach:        rule.TypeReachShapes,

	// Compiler options and the default library, through type_checking's builtin and specifier helpers.
	ProgramReads: rule.ReadsCompilerOptions | rule.ReadsDefaultLibrary,

	Run: func(ruleContext rule.Context, options rule.Given[NoBaseToStringOptions]) rule.Listeners {
		settings, isSettings := options.Read()
		if !isSettings {
			settings = DefaultNoBaseToStringSettings()
		}
		state := &noBaseToStringState{ruleContext: ruleContext, settings: settings}

		return rule.Listeners{
			// `a + b` and `a += b`, which our parser files under one kind rather than upstream's two.
			ast.KindBinaryExpression: func(node *ast.Node) {
				if ruleContext.TypeChecker == nil {
					return
				}
				binary := node.AsBinaryExpression()
				if binary.OperatorToken == nil {
					return
				}
				if binary.OperatorToken.Kind != ast.KindPlusToken &&
					binary.OperatorToken.Kind != ast.KindPlusEqualsToken {
					return
				}

				leftType := ruleContext.TypeChecker.GetTypeAtLocation(binary.Left)
				rightType := ruleContext.TypeChecker.GetTypeAtLocation(binary.Right)
				if leftType == nil || rightType == nil {
					return
				}

				// Only a concatenation with a string coerces the other side, and upstream checks the
				// left first, so `'' + {}` reports the right and `{} + ''` reports the left.
				if type_checking.GetTypeName(ruleContext.TypeChecker, leftType) == "string" {
					state.checkExpression(binary.Right, rightType)
					return
				}
				if binary.Left.Kind == ast.KindPrivateIdentifier {
					return
				}
				if type_checking.GetTypeName(ruleContext.TypeChecker, rightType) == "string" {
					state.checkExpression(binary.Left, leftType)
				}
			},

			ast.KindCallExpression: func(node *ast.Node) {
				if ruleContext.TypeChecker == nil {
					return
				}
				call := node.AsCallExpression()

				// `String(x)`, but only when `String` is the global rather than a local shadow.
				if state.isBuiltinStringCall(call) {
					argument := call.Arguments.Nodes[0]
					if argument.Kind != ast.KindSpreadElement {
						state.checkExpression(argument, nil)
					}
					return
				}

				// `x.join()`, `x.toString()`, `x.toLocaleString()`. Upstream reaches these through
				// three selectors on the member expression; here the call is the anchor and the
				// callee is read off it.
				callee := call.Expression
				if callee == nil || callee.Kind != ast.KindPropertyAccessExpression {
					return
				}
				access := callee.AsPropertyAccessExpression()
				name := access.Name()
				if name == nil || name.Kind != ast.KindIdentifier {
					return
				}
				receiver := access.Expression
				if receiver == nil {
					return
				}

				switch name.AsIdentifier().Text {
				case "join":
					// The constrained type, because a type parameter's constraint is what says
					// whether the elements stringify.
					receiverType := type_checking.GetConstrainedTypeAtLocation(ruleContext.TypeChecker, receiver)
					if receiverType == nil {
						return
					}
					state.checkExpressionForArrayJoin(receiver, receiverType)
				case "toString", "toLocaleString":
					state.checkExpression(receiver, nil)
				}
			},

			// A template literal coerces every interpolation, unless the whole thing is a tag's
			// argument, where the tag decides what to do with the raw parts.
			ast.KindTemplateExpression: func(node *ast.Node) {
				if ruleContext.TypeChecker == nil {
					return
				}
				if node.Parent != nil && node.Parent.Kind == ast.KindTaggedTemplateExpression {
					return
				}
				for _, span := range node.AsTemplateExpression().TemplateSpans.Nodes {
					expression := span.AsTemplateSpan().Expression
					if expression != nil {
						state.checkExpression(expression, nil)
					}
				}
			},
		}
	},
}

// combineNoBaseToStringUnion is upstream's `collectUnionTypeCertainty`.
func combineNoBaseToStringUnion(
	parts []*checker.Type,
	certaintyOf func(*checker.Type) noBaseToStringUsefulness,
) noBaseToStringUsefulness {
	allNever := true
	allAlways := true
	for _, part := range parts {
		switch certaintyOf(part) {
		case noBaseToStringNever:
			allAlways = false
		case noBaseToStringAlways:
			allNever = false
		default:
			allNever = false
			allAlways = false
		}
	}
	if allNever {
		return noBaseToStringNever
	}
	if allAlways {
		return noBaseToStringAlways
	}
	return noBaseToStringSometimes
}

// combineNoBaseToStringIntersection is upstream's `collectIntersectionTypeCertainty`.
//
// Any useful constituent makes the whole useful, because the intersection has that constituent's
// coercion method. Everything else is Never, including Sometimes, which is upstream's behavior and
// reads as a rounding-down rather than an oversight.
func combineNoBaseToStringIntersection(
	parts []*checker.Type,
	certaintyOf func(*checker.Type) noBaseToStringUsefulness,
) noBaseToStringUsefulness {
	for _, part := range parts {
		if certaintyOf(part) == noBaseToStringAlways {
			return noBaseToStringAlways
		}
	}
	return noBaseToStringNever
}

// noBaseToStringAliasOrSymbol is upstream's `type.aliasSymbol ?? type.getSymbol()`.
func noBaseToStringAliasOrSymbol(subjectType *checker.Type) *ast.Symbol {
	if alias := checker.Type_alias(subjectType); alias != nil {
		if symbol := alias.Symbol(); symbol != nil {
			return symbol
		}
	}
	return checker.Type_symbol(subjectType)
}

// noBaseToStringDeclarationCanHaveTypeParameters is upstream's `canHaveTypeParameters`.
func noBaseToStringDeclarationCanHaveTypeParameters(declaration *ast.Node) bool {
	switch declaration.Kind {
	case ast.KindTypeAliasDeclaration, ast.KindInterfaceDeclaration, ast.KindClassDeclaration:
		return true
	}
	return false
}

// noBaseToStringDeclarationHasTypeParameters is upstream's `decl.typeParameters` truthiness.
func noBaseToStringDeclarationHasTypeParameters(declaration *ast.Node) bool {
	parameters := declaration.TypeParameterList()
	return parameters != nil && len(parameters.Nodes) > 0
}

// noBaseToStringDeclaredOnObjectInterface answers upstream's
// `isInterfaceDeclaration(parent) && parent.name.text === 'Object'`.
func noBaseToStringDeclaredOnObjectInterface(declaration *ast.Node) bool {
	parent := declaration.Parent
	if parent == nil || parent.Kind != ast.KindInterfaceDeclaration {
		return false
	}
	name := parent.AsInterfaceDeclaration().Name()
	return name != nil && name.Kind == ast.KindIdentifier && name.AsIdentifier().Text == "Object"
}

// noBaseToStringNameIsIgnored is upstream's `ignoredTypeNames.includes(name)`.
func noBaseToStringNameIsIgnored(ignoredTypeNames []string, name string) bool {
	for _, ignored := range ignoredTypeNames {
		if ignored == name {
			return true
		}
	}
	return false
}

// isNoBaseToStringLiteral is upstream's `node.type === Literal` skip.
//
// Upstream's ESTree has one Literal node; ours splits every kind of literal onto its own, so the one
// test becomes this list. A template with no interpolation is a literal there and here.
func isNoBaseToStringLiteral(node *ast.Node) bool {
	switch node.Kind {
	case ast.KindStringLiteral,
		ast.KindNumericLiteral,
		ast.KindBigIntLiteral,
		ast.KindRegularExpressionLiteral,
		ast.KindNoSubstitutionTemplateLiteral,
		ast.KindNullKeyword,
		ast.KindTrueKeyword,
		ast.KindFalseKeyword:
		return true
	}
	return false
}

// noBaseToStringIsDefaultLibrarySymbol asks the program whether a symbol comes from the default library. It
// reads the program, so it lives beside the rule that declares that read: internal/guard counts a rule's reads
// in the file that defines it.
func noBaseToStringIsDefaultLibrarySymbol(ruleContext rule.Context, symbol *ast.Symbol) bool {
	return rule.IsSymbolFromDefaultLibrary(ruleContext.Program, symbol)
}

// noBaseToStringIsDefaultLibraryFile asks the program whether a file is part of the default library, beside
// the rule for the same reason as noBaseToStringIsDefaultLibrarySymbol.
func noBaseToStringIsDefaultLibraryFile(ruleContext rule.Context, sourceFile *ast.SourceFile) bool {
	return rule.IsSourceFileDefaultLibrary(ruleContext.Program, sourceFile)
}
