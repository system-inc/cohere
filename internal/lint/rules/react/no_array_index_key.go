package react

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
)

var messageNoArrayIndexKey = rule.Message{
	Id: "noArrayIndex",
	Description: "This key is the array index the item happens to sit at, which is not an " +
		"identity. When the list is reordered, filtered or has an item inserted, the same key " +
		"lands on a different item, so React reuses the wrong element and whatever state that " +
		"element held goes with it: a checked box, a focused input, a half-typed value. Nothing " +
		"errors, so the bug surfaces later as state attached to the wrong row. Key on something " +
		"belonging to the item instead, usually its identifier.",
}

// noArrayIndexKeyIteratorPositions maps each iterator method to the parameter position its index
// argument occupies.
//
// This is upstream's `iteratorFunctionsToIndexParamPosition` verbatim. The two folding methods take
// an accumulator first, which pushes their index to position two, and getting that wrong is
// invisible in the common case: `foo.reduce((accumulator, item) => ...)` has an index at neither
// position, so a port using position one for everything is silent exactly where upstream is silent
// and wrong on the shape that matters. Both are measured in the fixtures.
var noArrayIndexKeyIteratorPositions = map[string]int{
	"every":       1,
	"filter":      1,
	"find":        1,
	"findIndex":   1,
	"flatMap":     1,
	"forEach":     1,
	"map":         1,
	"reduce":      2,
	"reduceRight": 2,
	"some":        1,
}

// NoArrayIndexKey flags a React key derived from an array index.
//
//	valid:   foo.map((bar) => <Foo key={bar.id} />)
//	valid:   foo.map((bar, i) => <Foo key={bar[i]} />)
//	valid:   <Foo key={i} />                              (not inside an iterator callback)
//	valid:   foo.map((bar, i) => <Foo key="fixed" />)
//	invalid: foo.map((bar, i) => <Foo key={i} />)
//	invalid: foo.map((bar, i) => <Foo key={`x-${i}`} />)
//	invalid: foo.map((bar, i) => <Foo key={String(i)} />)
//	invalid: foo.map((bar, i) => React.createElement("div", { key: i }))
//
// Ported from `react/no-array-index-key` in `eslint-plugin-react`, read from the clone at
// `lib/rules/no-array-index-key.js`. `schema: []` means no options; one message; no fixer. Every
// behaviour below was measured by driving the installed build (7.37.5) through the ESLint Linter
// API rather than reasoned from the source.
//
// # There is no file-suffix gate
//
// Measured under `.tsx`, `.jsx` and `.js`: all three report. Three rules in this package used to
// carry such a gate as oxc residue and it was removed; nothing here reintroduces one.
//
// # The shape is a stack, and this tree has no exit listener
//
// Upstream pushes an index parameter name on entering an iterator call and pops it on exiting,
// then asks whether a key expression names anything currently on the stack. Our walk is pre-order
// with no exit hook, so the whole rule runs inside one `KindSourceFile` listener that walks the
// tree itself and maintains the stack across the recursion. That is the shape
// `no_multi_comp.go` and `no_class_assign.go` both use.
//
// The stack rather than a single name is load-bearing: nested iterators each contribute a name, and
// `foo.map((a, i) => bar.map((b, j) => <Foo key={i} />))` reports on the outer index from inside the
// inner callback. Measured.
//
// # Which calls push, and the two-argument form
//
// The callee must be a property access whose property names one of the ten iterator methods above,
// and the callback must be a function expression or arrow with enough parameters to have an index
// at that method's position. Measured silent: `foo.each(...)` and `foo.sort(...)` are not in the
// set, `foo.map((bar) => ...)` has too few parameters, `foo.map(cb)` passes a reference rather than
// a literal function, and `foo["map"](...)` is a computed access whose property is not an
// identifier.
//
// `React.Children.map` and `Children.map` take the collection first and the callback second, so the
// callback argument index shifts. Upstream detects this by asking whether the callee's object is
// named `Children`, or is itself a member expression whose own object is the React pragma. Measured:
// `Children.map(c, (child, i) => ...)` and `React.Children.map(...)` both report, while
// `Other.map(c, (child, i) => ...)` does not, because for any other receiver the callback is read
// from argument zero and argument zero is the collection.
//
// # What counts as using the index, and the four shapes upstream accepts
//
//	key={i}                 the identifier itself
//	key={`x-${i}`}          any template expression naming it
//	key={'x-' + i}          any identifier anywhere in a binary expression tree
//	key={i.toString()}      that exact method name on the index
//	key={String(i)}         that exact function name with the index first
//
// Everything else is silent, and the near misses are the interesting part. Measured:
// `key={Number(i)}` and `key={i.toFixed()}` are both clean, because upstream hardcodes `String` and
// `toString` rather than reasoning about conversion; `key={bar.toString()}` is clean because the
// receiver is not the index; and `key={bar[i]}` is clean because an element access is not one of
// the five shapes, even though the index is plainly in it. A port that generalized any of these
// would report on code upstream leaves alone.
//
// # A template or binary expression reports once per index reference
//
// Upstream filters the expression list and reports inside a `forEach`, so a template naming the
// index twice reports twice, both times on the whole template. Measured, and no corpus case has
// more than one reference.
//
// # createElement and cloneElement
//
// A call to `React.createElement` or `React.cloneElement` with at least two arguments has its
// second argument read as a props object, and a `key` property in it is checked the same way. This
// only applies inside an iterator callback: the same call at the top level is silent, because the
// stack is empty. Measured, including that a spread-only props object is silent and a computed
// `["key"]` property is silent.
//
// Upstream also accepts a bare `cloneElement` imported from `react`, resolved through its own
// variable index. The checker answers the same question: a bare identifier qualifies when it
// resolves to an import whose module specifier is exactly `react`. Measured both ways, with an
// import from another module silent.
//
// # Where the finding points
//
// Four of the five shapes report the whole key expression; `String(i)` reports the argument rather
// than the call, which is upstream passing `node.arguments[0]` at that one site and the whole node
// at the other four. A message-id fixture cannot see the difference and the corpus does not assert
// it, so the span cases below do.
var NoArrayIndexKey = rule.Rule[rule.NoOptions]{
	Name:             "react/no-array-index-key",
	NeedsTypeChecker: true,
	Run: func(ruleContext rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners {
		if ruleContext.TypeChecker == nil {
			return nil
		}

		return rule.Listeners{
			// One listener over the whole file, because the rule is a stack and there is no exit
			// hook to pop it with. `KindSourceFile` fires before its children, so the walk below is
			// the rule's own rather than the harness's.
			ast.KindSourceFile: func(node *ast.Node) {
				walker := &noArrayIndexKeyWalker{ctx: ruleContext}
				walker.walk(node)
			},
		}
	},
}

// noArrayIndexKeyIsReactChildrenCall reports whether a callee is the React children iterator, whose
// callback is the second argument rather than the first.
//
// Upstream restricts this to `map` and `forEach`, which is narrower than the ten-method set the
// caller has already matched, so `Children.filter(...)` takes the ordinary argument order.
func noArrayIndexKeyIsReactChildrenCall(access *ast.PropertyAccessExpression) bool {
	methodName := access.Name()
	if methodName == nil || methodName.Kind != ast.KindIdentifier {
		return false
	}
	if methodName.Text() != "map" && methodName.Text() != "forEach" {
		return false
	}

	receiver := access.Expression
	if receiver == nil {
		return false
	}

	// `Children.map(...)`.
	if receiver.Kind == ast.KindIdentifier {
		return receiver.Text() == "Children"
	}

	// `React.Children.map(...)`: the receiver is itself a property access whose own object is the
	// React pragma. Upstream never checks that the middle name is `Children` here, only that the
	// outermost object is React, and that looseness is reproduced.
	if receiver.Kind == ast.KindPropertyAccessExpression {
		inner := receiver.AsPropertyAccessExpression().Expression
		return inner != nil && inner.Kind == ast.KindIdentifier && inner.Text() == "React"
	}
	return false
}

// noArrayIndexKeyImportModuleSpecifier walks from an import specifier up to its declaration's module
// string, or nil.
//
// The chain is specifier to named-bindings to import-clause to import-declaration, and every step
// is checked rather than assumed, because a malformed import recovers into shapes well-formed source
// never produces.
func noArrayIndexKeyImportModuleSpecifier(specifier *ast.Node) *ast.Node {
	for current := specifier; current != nil; current = current.Parent {
		if current.Kind == ast.KindImportDeclaration {
			return current.AsImportDeclaration().ModuleSpecifier
		}
	}
	return nil
}

// noArrayIndexKeyIsLogicalOperator reports whether an operator makes this an ESTree
// `LogicalExpression` rather than a `BinaryExpression`.
//
// The distinction has no meaning in our tree, where every one of these is a `KindBinaryExpression`,
// and it decides the rule's verdict because upstream dispatches on the node type. Measured against
// the installed build: a key built with `+` reports and one built with `??`, `||` or `&&` does not.
func noArrayIndexKeyIsLogicalOperator(kind ast.Kind) bool {
	switch kind {
	case ast.KindAmpersandAmpersandToken,
		ast.KindBarBarToken,
		ast.KindQuestionQuestionToken:
		return true
	}
	return false
}
