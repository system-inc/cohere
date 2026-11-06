package typescript

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/system-inc/cohere/internal/lint/checking"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// StrictVoidReturnOptions is the rule's one option.
type StrictVoidReturnOptions struct {
	// AllowReturnAny permits a function returning `any` where a void function is expected.
	//
	// Defaults to false. The generic decoder would be correct by coincidence here; the hand-rolled
	// one keeps an absent key distinguishable from an explicit false.
	AllowReturnAny bool
}

// DefaultStrictVoidReturnSettings is upstream's `defaultOptions`.
func DefaultStrictVoidReturnSettings() StrictVoidReturnOptions {
	return StrictVoidReturnOptions{AllowReturnAny: false}
}

type strictVoidReturnRawOptions struct {
	AllowReturnAny *bool `json:"allowReturnAny"`
}

// DecodeStrictVoidReturnOptions reads the rule's configuration.
//
// cohere's config layer strips ESLint's `[severity, options]` tuple, so the bare object arrives.
func DecodeStrictVoidReturnOptions(raw []byte) (StrictVoidReturnOptions, error) {
	decoded, decodeOptionsError := rule.DecodeOptionsInto[strictVoidReturnRawOptions]()(raw)
	if decodeOptionsError != nil {
		return DefaultStrictVoidReturnSettings(), decodeOptionsError
	}
	options := DefaultStrictVoidReturnSettings()
	if decoded.AllowReturnAny != nil {
		options.AllowReturnAny = *decoded.AllowReturnAny
	}
	return options, nil
}

// StrictVoidReturn flags a value-returning function passed where a void function is expected.
//
//	valid:   declare function takes(cb: () => void): void; takes(() => {});
//	valid:   declare function takes(cb: () => number): void; takes(() => 1);
//	invalid: declare function takes(cb: () => void): void; takes(() => 1);
//	invalid: declare function takes(cb: () => void): void; takes(async () => {});
//
// A callback typed `() => void` is a promise that its result is ignored. Passing something that
// returns a value there is usually a mistake with consequences the type system will not raise: an
// async callback passed to `addEventListener` produces a floating promise whose rejection nobody
// handles, and a value-returning callback passed to `forEach` reads as though the value matters.
//
// # The whole rule turns on the contextual type
//
// Every listener finds a position where a value is being placed, asks what type is expected there,
// and reports when that expectation is a void-returning function while the value supplied is not.
// That expectation comes from the checker rather than from syntax, which is why this needs types.
//
// `getContextualType` is the load-bearing call and it is reachable here, which was established with
// a compiling probe before this rule was written rather than assumed from a grep. The shim spells
// checker methods `Checker_getX`, so searching for `GetContextualType` finds nothing and reports a
// false absence; the control on that same search also returned zero, which is the only reason the
// absence was not recorded as real.
//
// # Three findings, and they point at three different things
//
//	nonVoidFunc     the function is not a literal, or is a generator, or has a non-void return
//	                annotation. Points at the function, or at the ANNOTATION when there is one.
//	asyncFunc       an async function literal. Points at the function HEAD rather than the whole
//	                function, which is a narrower span than the node.
//	nonVoidReturn   a specific `return` statement inside an otherwise-acceptable function. Points at
//	                the `return` KEYWORD, not the statement and not the returned expression.
//
// Those spans are upstream's and each is measured. A port reporting all three on the node would
// satisfy every message-id assertion while pointing a reader at the wrong token three ways.
//
// # What counts as already-void
//
// `void`, `never` and `undefined` are all acceptable return types, and `any` joins them only when
// `allowReturnAny` is set. The check is over every constituent of every call signature's return
// type, so a union reporting requires every member to be acceptable.
//
// # Cost
//
// Twelve anchors, most of them common, but each exits on a cheap syntactic test before the
// contextual type is requested. The checker is consulted only for an expression sitting in a
// position that could expect a function.
var StrictVoidReturn = rule.Rule[StrictVoidReturnOptions]{
	Name: "@typescript-eslint/strict-void-return",

	// The contextual type decides every finding.
	NeedsTypeChecker: true,
	TypeReach:        rule.TypeReachShapes,

	Run: func(ruleContext rule.Context, options rule.Given[StrictVoidReturnOptions]) rule.Listeners {
		settings, isSettings := options.Read()
		if !isSettings {
			settings = DefaultStrictVoidReturnSettings()
		}

		state := &strictVoidReturnState{ruleContext: ruleContext, settings: settings}

		return rule.Listeners{
			ast.KindCallExpression: state.checkCall,
			ast.KindNewExpression:  state.checkCall,

			ast.KindArrayLiteralExpression: func(node *ast.Node) {
				for _, element := range node.AsArrayLiteralExpression().Elements.Nodes {
					if element.Kind != ast.KindSpreadElement {
						state.checkExpression(element)
					}
				}
			},

			ast.KindArrowFunction: func(node *ast.Node) {
				// A concise arrow body is an expression in a position that may expect a function.
				body := node.AsArrowFunction().Body
				if body != nil && body.Kind != ast.KindBlock {
					state.checkExpression(body)
				}
			},

			ast.KindBinaryExpression: func(node *ast.Node) {
				binary := node.AsBinaryExpression()
				if binary.OperatorToken == nil {
					return
				}
				// Upstream checks every AssignmentExpression and notes that arithmetic compounds
				// like `+=` fall out on their own, because their contextual type is never a
				// function. The logical compounds do not fall out: `foo ??= cb` places `cb` in a
				// position typed by `foo`, so a void-returning `foo` makes it this rule's question.
				// Measured, three corpus cases turn on exactly that and were silent when this arm
				// accepted only a plain `=`.
				switch binary.OperatorToken.Kind {
				case ast.KindEqualsToken,
					ast.KindQuestionQuestionEqualsToken,
					ast.KindBarBarEqualsToken,
					ast.KindAmpersandAmpersandEqualsToken:
					state.checkExpression(binary.Right)
				}
			},

			ast.KindObjectLiteralExpression: func(node *ast.Node) {
				for _, property := range node.AsObjectLiteralExpression().Properties.Nodes {
					switch property.Kind {
					case ast.KindPropertyAssignment:
						state.checkExpression(property.AsPropertyAssignment().Initializer)
					case ast.KindMethodDeclaration:
						// A shorthand method is a function in a property position, so it is checked
						// like one. `{ cb(n) { return n; } }` against a `(n: number) => void`
						// property reports, and it is not reachable through the assignment arm
						// because there is no initializer expression to check.
						state.reportIfNonVoidFunctionAtProperty(property)
					}
				}
			},

			ast.KindReturnStatement: func(node *ast.Node) {
				if argument := node.AsReturnStatement().Expression; argument != nil {
					state.checkExpression(argument)
				}
			},

			ast.KindVariableDeclaration: func(node *ast.Node) {
				if initializer := node.AsVariableDeclaration().Initializer; initializer != nil {
					state.checkExpression(initializer)
				}
			},

			ast.KindPropertyDeclaration: func(node *ast.Node) {
				// A class property is checked against the same-named member of every base type
				// first, because a subclass narrowing an inherited void callback to a value-
				// returning one is the mistake this arm exists for. Only if no base expects a void
				// function does the ordinary contextual-type path apply.
				if state.checkAgainstBaseTypes(node) {
					return
				}
				if initializer := node.AsPropertyDeclaration().Initializer; initializer != nil {
					state.checkExpression(initializer)
				}
			},

			ast.KindMethodDeclaration: func(node *ast.Node) {
				// A method with no body is an overload signature and declares nothing to check.
				if node.AsMethodDeclaration().Body == nil {
					return
				}
				state.checkAgainstBaseTypes(node)
			},

			ast.KindGetAccessor: func(node *ast.Node) {
				if node.AsGetAccessorDeclaration().Body == nil {
					return
				}
				state.checkAgainstBaseTypes(node)
			},

			ast.KindJsxAttribute: func(node *ast.Node) {
				initializer := node.AsJsxAttribute().Initializer
				if initializer == nil || initializer.Kind != ast.KindJsxExpression {
					return
				}
				// The attribute's expected type resolves the element's props, for every attribute,
				// and a value that is not a non-void function is silent whatever was expected, so
				// the value is asked first (#hekjpw3).
				if inner := initializer.AsJsxExpression().Expression; inner != nil &&
					state.isNonVoidFunctionValue(inner) {
					state.checkExpression(inner)
				}
			},
		}
	},
}

// strictVoidReturnPropertyName reads a property key that names something a type can carry.
//
// A numeric key counts: `{ 1234(n) { return n } }` against a `{ 1234: (n: number) => void }` type
// reports, and the property symbol is spelled with the digits.
func strictVoidReturnPropertyName(name *ast.Node) (string, bool) {
	switch name.Kind {
	case ast.KindIdentifier, ast.KindStringLiteral, ast.KindNumericLiteral,
		ast.KindNoSubstitutionTemplateLiteral:
		return name.Text(), true
	}
	return "", false
}

// strictVoidReturnHeritageClauses reads the heritage clauses of a class-like declaration.
func strictVoidReturnHeritageClauses(node *ast.Node) *ast.NodeList {
	switch node.Kind {
	case ast.KindClassDeclaration:
		return node.AsClassDeclaration().HeritageClauses
	case ast.KindClassExpression:
		return node.AsClassExpression().HeritageClauses
	}
	return nil
}

func strictVoidReturnIsVoid(subject *checker.Type) bool {
	return type_checking.IsTypeFlagSet(subject, checker.TypeFlagsVoid)
}

func strictVoidReturnIsNullishOrAny(subject *checker.Type) bool {
	return type_checking.IsTypeFlagSet(subject,
		checker.TypeFlagsVoidLike|checker.TypeFlagsUndefined|checker.TypeFlagsNull|
			checker.TypeFlagsAny|checker.TypeFlagsNever)
}

func strictVoidReturnIsGenerator(node *ast.Node) bool {
	switch node.Kind {
	case ast.KindFunctionExpression:
		return node.AsFunctionExpression().AsteriskToken != nil
	case ast.KindMethodDeclaration:
		return node.AsMethodDeclaration().AsteriskToken != nil
	}
	return false
}

func strictVoidReturnIsAsync(node *ast.Node) bool {
	modifiers := node.Modifiers()
	if modifiers == nil {
		return false
	}
	for _, modifier := range modifiers.Nodes {
		if modifier.Kind == ast.KindAsyncKeyword {
			return true
		}
	}
	return false
}

func strictVoidReturnTypeAnnotation(node *ast.Node) *ast.Node {
	switch node.Kind {
	case ast.KindArrowFunction:
		return node.AsArrowFunction().Type
	case ast.KindFunctionExpression:
		return node.AsFunctionExpression().Type
	case ast.KindMethodDeclaration:
		return node.AsMethodDeclaration().Type
	case ast.KindGetAccessor:
		return node.AsGetAccessorDeclaration().Type
	}
	return nil
}

func strictVoidReturnBody(node *ast.Node) *ast.Node {
	switch node.Kind {
	case ast.KindArrowFunction:
		return node.AsArrowFunction().Body
	case ast.KindFunctionExpression:
		return node.AsFunctionExpression().Body
	case ast.KindMethodDeclaration:
		return node.AsMethodDeclaration().Body
	case ast.KindGetAccessor:
		return node.AsGetAccessorDeclaration().Body
	}
	return nil
}

// strictVoidReturnHeadRange is upstream's `getFunctionHeadLoc`.
//
// The reported span for an async or generator function literal is its `HEAD` rather than the whole
// function, so a multi-line callback underlines its first line instead of its body. The head runs
// from the function's first token through the closing parenthesis of its parameter list.
func strictVoidReturnHeadRange(ruleContext rule.Context, node *ast.Node) core.TextRange {
	nodeRange := rule.TokenRange(ruleContext.SourceFile, node)
	body := strictVoidReturnBody(node)
	if body == nil {
		return nodeRange
	}
	// Everything up to the body, with trailing trivia and any arrow trimmed off.
	end := rule.TokenRange(ruleContext.SourceFile, body).Pos()
	text := ruleContext.SourceFile.Text()
	for end > nodeRange.Pos() {
		trimmed := end - 1
		switch text[trimmed] {
		case ' ', '\t', '\r', '\n', '>', '=':
			end = trimmed
			continue
		}
		break
	}
	if end <= nodeRange.Pos() {
		return nodeRange
	}
	return core.NewTextRange(nodeRange.Pos(), end)
}

// strictVoidReturnWalkStatements collects the return statements belonging to one function body.
//
// It descends through ordinary statement nesting but never into a nested function, whose returns
// are its own. Upstream's `walkStatements` makes the same distinction.
func strictVoidReturnWalkStatements(body *ast.Node) []*ast.Node {
	found := []*ast.Node{}
	var walk func(*ast.Node)
	walk = func(node *ast.Node) {
		if node == nil {
			return
		}
		switch node.Kind {
		case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction,
			ast.KindClassDeclaration, ast.KindClassExpression:
			return
		case ast.KindReturnStatement:
			found = append(found, node)
			return
		}
		node.ForEachChild(func(child *ast.Node) bool {
			walk(child)
			return false
		})
	}
	walk(body)
	return found
}

func strictVoidReturnNonVoidFuncMessage() rule.Message {
	return rule.Message{
		Id:          "nonVoidFunc",
		Description: "Value-returning function used in a context where a void function is expected.",
	}
}

func strictVoidReturnAsyncFuncMessage() rule.Message {
	return rule.Message{
		Id:          "asyncFunc",
		Description: "Async function used in a context where a void function is expected.",
	}
}

func strictVoidReturnNonVoidReturnMessage() rule.Message {
	return rule.Message{
		Id:          "nonVoidReturn",
		Description: "Value returned in a context where a void return is expected.",
	}
}
