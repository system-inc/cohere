package typescript

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/scanner"
	"github.com/system-inc/cohere/internal/rule"
)

// NoExtraneousClassOptions is the rule's option surface.
//
// All four default to FALSE, so the generic decoder would happen to produce the right zero value for
// every one of them. The decoder below is hand-rolled anyway: a decoder that silently dropped all
// four would be indistinguishable from a correct one on every input, and the next key added here may
// default to true.
type NoExtraneousClassOptions struct {
	// AllowConstructorOnly permits a class whose only member is a constructor.
	AllowConstructorOnly bool
	// AllowEmpty permits a class with no members at all.
	AllowEmpty bool
	// AllowStaticOnly permits a class whose members are all static.
	AllowStaticOnly bool
	// AllowWithDecorator permits any class carrying a decorator, whatever its members.
	AllowWithDecorator bool
}

// DefaultNoExtraneousClassSettings is upstream's `defaultOptions`.
func DefaultNoExtraneousClassSettings() NoExtraneousClassOptions {
	return NoExtraneousClassOptions{}
}

// noExtraneousClassRawOptions is the wire shape, with pointers so an absent key stays
// distinguishable from an explicit false.
type noExtraneousClassRawOptions struct {
	AllowConstructorOnly *bool `json:"allowConstructorOnly"`
	AllowEmpty           *bool `json:"allowEmpty"`
	AllowStaticOnly      *bool `json:"allowStaticOnly"`
	AllowWithDecorator   *bool `json:"allowWithDecorator"`
}

// DecodeNoExtraneousClassOptions reads the rule's configuration.
//
// cohere's config layer strips ESLint's `[severity, options]` tuple before dispatch, so what arrives
// is the bare object rather than upstream's one-element array.
func DecodeNoExtraneousClassOptions(raw []byte) (any, error) {
	decoded, err := rule.DecodeOptionsInto[noExtraneousClassRawOptions]()(raw)
	if err != nil {
		return DefaultNoExtraneousClassSettings(), err
	}

	wire, _ := decoded.(noExtraneousClassRawOptions)
	options := DefaultNoExtraneousClassSettings()
	if wire.AllowConstructorOnly != nil {
		options.AllowConstructorOnly = *wire.AllowConstructorOnly
	}
	if wire.AllowEmpty != nil {
		options.AllowEmpty = *wire.AllowEmpty
	}
	if wire.AllowStaticOnly != nil {
		options.AllowStaticOnly = *wire.AllowStaticOnly
	}
	if wire.AllowWithDecorator != nil {
		options.AllowWithDecorator = *wire.AllowWithDecorator
	}
	return options, nil
}

// NoExtraneousClass flags a class that exists only to be a namespace.
//
//	valid:   class Foo { public prop = 1; constructor() {} }
//	valid:   class Foo { constructor(public bar: string) {} }    a parameter property is real state
//	valid:   class Bar extends Base { static helper(): void {} } a subclass is never extraneous
//	valid:   abstract class Foo { abstract property: string; }
//	invalid: class Foo {}
//	invalid: class Foo { constructor() {} }
//	invalid: export class Bar { static helper(): void {} }
//
// A class holding nothing but statics is a module written with extra ceremony: the language already
// has a namespace, it is called a file, and its members are tree-shakeable in a way a class's
// statics are not. A class holding nothing at all, or nothing but an empty constructor, is a
// placeholder somebody forgot to remove or to turn into a function.
//
// # Three findings, and the option surface changes which one rather than only whether
//
//	empty             no members at all
//	onlyConstructor   every member is a constructor taking no parameter properties
//	onlyStatic        every member is static
//
// Each has its own allow option, plus a fourth that exempts any decorated class outright. The
// decorated exemption is checked before the member walk, so a decorated empty class is silent under
// allowWithDecorator and reports `empty` without it.
//
// # A superclass exempts unconditionally and a decorator does not
//
// `parent.superClass` short-circuits with no option behind it, because a subclass inherits behavior
// and is not a namespace whatever its own body holds. An `implements` clause is NOT a superclass and
// does not exempt, which our tree spells as a different heritage token rather than a different
// field, so the check reads the token instead of asking whether a heritage clause exists.
//
// # Where our parser spells things differently, and it is most of the member classification
//
// Upstream reads ESTree node TYPES to classify a member. Our parser puts the same information in
// MODIFIERS on two node kinds, so the mapping is not one to one:
//
//	upstream                          ours
//	AccessorProperty                  KindPropertyDeclaration + accessor modifier
//	TSAbstractPropertyDefinition      KindPropertyDeclaration + abstract modifier
//	TSAbstractMethodDefinition        KindMethodDeclaration + abstract modifier
//	TSAbstractAccessorProperty        KindPropertyDeclaration + abstract and accessor modifiers
//	PropertyDefinition/MethodDefinition, `static`   the same kinds, `ast.IsStatic`
//
// Upstream's condition reads "not static, OR one of the abstract node types, OR an index signature".
// The abstract arms are listed separately there because an abstract member cannot be static in
// TypeScript, so upstream cannot express them through its own static test and says so at the line,
// citing the compiler issue.
//
// The obvious conclusion is that our static test covers them and the abstract arm here is redundant.
// It is not, and the measurement is recorded beside that arm: our parser recovers from the illegal
// `static abstract` and produces a member carrying both modifiers, so the arm is reachable and
// removing it makes this rule report where upstream is silent.
//
// # The finding anchors on the class NAME, or on the whole class when there is none
//
// `parent.id` when the parent is a declaration with a name, and the parent itself otherwise. That
// second case is reachable: `export default class { static hello() {} }` has no name and is reported
// across its entire body, which the fixtures assert as a multi-line span beside a three-character one.
//
// # Cost
//
// One listener on each class kind, both rare, and the member walk breaks as soon as neither
// classification can still hold. No checker, no program, no per-file state.
var NoExtraneousClass = rule.Rule{
	Name: "@typescript-eslint/no-extraneous-class",

	Run: func(ctx rule.Context, options any) rule.Listeners {
		settings, isSettings := options.(NoExtraneousClassOptions)
		if !isSettings {
			settings = DefaultNoExtraneousClassSettings()
		}

		// judge is shared by the two class kinds, which differ only in whether a name is required.
		// Upstream anchors on the class BODY and reads its parent; ours has no body node, so the
		// listener is on the class itself and the members are read directly.
		judge := func(node *ast.Node, name *ast.Node, members *ast.NodeList, heritageClauses *ast.NodeList) {
			if members == nil {
				return
			}

			if hasSuperclass(heritageClauses) {
				return
			}
			if settings.AllowWithDecorator && hasDecorator(node) {
				return
			}

			// Upstream's `parent.type === ClassDeclaration && parent.id ? parent.id : parent`.
			//
			// Both halves of that test matter and the second is easy to drop. A class EXPRESSION is
			// reported on the whole class even when it has a name, because the type test excludes it
			// before the name is consulted, so `const Foo = class Named { static x() {} }` reports
			// across the entire expression rather than on `Named`. Measured against the installed
			// build and confirmed in its parser's own output, which calls that node a ClassExpression
			// carrying an id. Upstream's corpus writes no named class expression at all, so nothing
			// imported can see a port that reads the name here.
			reportRange := classAnchorRange(ctx, node)
			if node.Kind == ast.KindClassDeclaration && name != nil {
				reportRange = rule.TokenRange(ctx.SourceFile, name)
			}

			if len(members.Nodes) == 0 {
				if settings.AllowEmpty {
					return
				}
				ctx.ReportRange(reportRange, noExtraneousClassEmptyMessage)
				return
			}

			onlyStatic := true
			onlyConstructor := true
			for _, member := range members.Nodes {
				if member.Kind == ast.KindConstructor {
					// A constructor declaring a parameter property is declaring real instance
					// state, so the class is neither constructor-only nor static-only. Upstream
					// leaves both flags alone for a plain constructor, which is what lets a class
					// whose only member is one stay `onlyStatic` as well as `onlyConstructor`.
					if constructorDeclaresParameterProperty(member) {
						onlyConstructor = false
						onlyStatic = false
					}
				} else {
					onlyConstructor = false
					if isInstanceLevelClassMember(member) {
						onlyStatic = false
					}
				}
				// Upstream's early exit, and it is a pure optimization rather than a decision.
				//
				// A mutation removing it survives every fixture, and that survival is equivalence
				// rather than a blind spot. Both flags start true and the loop body only ever writes
				// false to them, so once both are false no later member can change either, and the
				// state at this break is by construction the state at the loop's natural end. There
				// is no input on which the two versions differ, which is why no fixture was added for
				// it. Kept because it is upstream's and because a class with many members stops
				// walking as soon as the answer is settled.
				if !onlyStatic && !onlyConstructor {
					break
				}
			}

			if onlyConstructor {
				if !settings.AllowConstructorOnly {
					ctx.ReportRange(reportRange, noExtraneousClassOnlyConstructorMessage)
				}
				return
			}
			if onlyStatic && !settings.AllowStaticOnly {
				ctx.ReportRange(reportRange, noExtraneousClassOnlyStaticMessage)
			}
		}

		return rule.Listeners{
			ast.KindClassDeclaration: func(node *ast.Node) {
				declaration := node.AsClassDeclaration()
				judge(node, declaration.Name(), declaration.Members, declaration.HeritageClauses)
			},
			ast.KindClassExpression: func(node *ast.Node) {
				expression := node.AsClassExpression()
				judge(node, expression.Name(), expression.Members, expression.HeritageClauses)
			},
		}
	},
}

var noExtraneousClassEmptyMessage = rule.Message{
	Id:          "empty",
	Description: "Unexpected empty class.",
}

var noExtraneousClassOnlyConstructorMessage = rule.Message{
	Id:          "onlyConstructor",
	Description: "Unexpected class with only a constructor.",
}

var noExtraneousClassOnlyStaticMessage = rule.Message{
	Id:          "onlyStatic",
	Description: "Unexpected class with only static properties.",
}

// classAnchorRange is the span upstream reports when a class has no name to report on.
//
// It begins at the `class` keyword rather than at the node, and the difference is `export default`.
// ESTree wraps a default-exported class in an ExportDefaultDeclaration, so upstream's `parent` is the
// inner class and its span starts at `class`. Ours puts `export` and `default` in the class node's own
// modifier list, so `rule.TokenRange` on the node includes them and the span is eleven characters
// longer than the one upstream records. Caught by the corpus rather than by reading: upstream's
// `export default class { static hello() {} }` case records a span running from line 2 column 16, and
// column 16 is where `class` begins.
//
// The same correction covers a decorator, which our parser also files as a modifier, though no
// upstream case exercises that combination because a decorated class in the corpus always has a name.
func classAnchorRange(ctx rule.Context, node *ast.Node) core.TextRange {
	modifiers := node.Modifiers()
	if modifiers == nil || len(modifiers.Nodes) == 0 {
		return rule.TokenRange(ctx.SourceFile, node)
	}
	keyword := scanner.GetRangeOfTokenAtPosition(ctx.SourceFile, modifiers.End())
	return keyword.WithEnd(node.End())
}

// hasSuperclass is upstream's `parent.superClass`.
//
// ESTree carries a superclass in its own field, so upstream reads one property. Ours puts both
// `extends` and `implements` in the same heritage list and separates them by TOKEN, so the token is
// what has to be read: an `implements` clause is not a superclass and does not exempt, and a check
// that merely asked whether a heritage list existed would silently exempt every class implementing
// an interface.
func hasSuperclass(heritageClauses *ast.NodeList) bool {
	if heritageClauses == nil {
		return false
	}
	for _, clause := range heritageClauses.Nodes {
		if clause.AsHeritageClause().Token == ast.KindExtendsKeyword {
			return true
		}
	}
	return false
}

// hasDecorator is upstream's `node.decorators.length !== 0`.
//
// Our parser puts a decorator in the same modifier list as `export`, `abstract` and the rest, so this
// looks for the kind rather than reading a separate field.
func hasDecorator(node *ast.Node) bool {
	modifiers := node.Modifiers()
	if modifiers == nil {
		return false
	}
	for _, modifier := range modifiers.Nodes {
		if modifier.Kind == ast.KindDecorator {
			return true
		}
	}
	return false
}

// constructorDeclaresParameterProperty answers upstream's
// `prop.value.params.some(param => param.type === TSParameterProperty)`.
//
// ESTree wraps a parameter carrying an accessibility or readonly modifier in its own node type. Ours
// leaves it a plain parameter and puts the modifier on it, so the question becomes whether the
// parameter has any of the modifiers that make it a property. A decorator on a parameter does NOT
// make one, which is why this names the modifiers rather than asking whether the list is non-empty.
func constructorDeclaresParameterProperty(constructor *ast.Node) bool {
	parameters := constructor.AsConstructorDeclaration().Parameters
	if parameters == nil {
		return false
	}
	for _, parameter := range parameters.Nodes {
		modifiers := parameter.Modifiers()
		if modifiers == nil {
			continue
		}
		for _, modifier := range modifiers.Nodes {
			switch modifier.Kind {
			case ast.KindPublicKeyword,
				ast.KindPrivateKeyword,
				ast.KindProtectedKeyword,
				ast.KindReadonlyKeyword,
				ast.KindOverrideKeyword:
				return true
			}
		}
	}
	return false
}

// isInstanceLevelClassMember answers upstream's condition for clearing `onlyStatic`.
//
// Upstream reads node types; ours reads kinds plus modifiers, and the mapping is in the rule's doc
// comment above. The abstract arms are deliberately redundant with the static test, for the reason
// recorded there.
func isInstanceLevelClassMember(member *ast.Node) bool {
	switch member.Kind {
	case ast.KindPropertyDeclaration,
		ast.KindMethodDeclaration,
		ast.KindGetAccessor,
		ast.KindSetAccessor:
		if !ast.IsStatic(member) {
			return true
		}
		// `static abstract` is not legal TypeScript, so this line reads as dead: upstream lists its
		// abstract node types separately only because it cannot express them through its own static
		// test, and our static test above would cover them on its own.
		//
		// It is not dead, and a mutation sweep is what asked the question. Our parser RECOVERS from
		// the illegal source rather than refusing the member, and hands back a property carrying both
		// modifiers, in either written order, for properties, methods and accessors alike. Measured
		// over five parses. Upstream is silent on all of those and reports on a legal static member in
		// the same run, so without this line the rule reports where upstream does not. The grammar
		// says one thing and the parser does another, and only the parser decides what a rule sees.
		return ast.HasSyntacticModifier(member, ast.ModifierFlagsAbstract)

	case ast.KindIndexSignature:
		// An index signature describes instance shape and has no static form upstream recognizes.
		return true
	}
	return false
}
