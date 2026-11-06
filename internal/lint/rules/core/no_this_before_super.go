package core

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// NoThisBeforeSuper requires a derived class constructor to call `super()` before it touches
// `this` or `super`.
//
// In a derived constructor the binding for `this` does not exist until `super()` returns. Reading
// it first is not a style problem: it throws a ReferenceError at runtime, on the first construction,
// every time. The rule exists because the failure is invisible in review and the stack trace points
// at the property access rather than at the missing call.
//
// Examples of incorrect code:
//
//	class A extends B { constructor() { this.c = 0; super(); } }
//	class A extends B { constructor() { super.c(); } }
//	class A extends B { constructor() { super(this.c); } }
//	class A extends B { constructor() { if (a) super(); this.a(); } }
//
// Examples of correct code:
//
//	class A extends B { constructor() { super(); this.c = 0; } }
//	class A { constructor() { this.c = 0; } }
//	class A extends null { constructor() { } }
//	class A extends B { constructor() { var c = () => this.d(); super(); } }
//
// # Reporting granularity, and why oxc rather than ESLint
//
// The two upstreams disagree about what a finding is, and the disagreement is visible rather than
// hidden. ESLint reports once per offending `this` or `super` node, with the message
// `'this' is not allowed before 'super()'`. Oxc reports once per offending *constructor*, anchored
// on the whole method definition.
//
// Oxc's granularity is reproduced here, because the snapshot is what pins the port: 26 fail inputs
// producing exactly 26 diagnostics, several of which contain two offending nodes.
// `class A extends B { constructor() { super(this.c); } }` has one, but
// `try { super(); } finally { this.a; }` reaches its `this` on two paths and still reports once.
// Choosing ESLint's granularity would make the imported corpus unable to check the port, which is
// the only thing the corpus is for.
//
// The cost is a wider caret. A reader gets the constructor underlined rather than the offending
// access, and in a long constructor that is less precise than it could be. It is stated here rather
// than quietly improved on, because a divergence that is written down is a decision and one that is
// not is a defect.
//
// # There is no control flow graph here, and the question does not need one
//
// Upstream reaches for oxc's CFG (`ctx.cfg()`), partitions basic blocks into "super was called" and
// "a violation happened locally", then walks edges propagating the first set across the second. We
// have no CFG. TypeScript's flow graph is not a substitute and this was measured rather than
// assumed: `ast.FlowNode` carries `Antecedent` and `Antecedents` and no successor edges anywhere in
// `TypeScript/tsc/internal/ast/flow.go`, because it exists for *narrowing* and is keyed on the
// expression positions where a type could change. There is no `super()` instruction in it to find.
//
// So the analysis here is structural, over the statement tree, and it reproduces upstream's verdict
// on every discrimination the corpus draws. The question this rule asks turns out to be a good fit
// for the tree, better than `getter-return`'s was, because it decomposes into two independent
// passes that each read the tree the obvious way:
//
//	definitelyCallsSuper(statement)   does every path through this statement call super()?
//	findViolation(statement)          walking in source order, is a this/super reached
//	                                  while definitelyCallsSuper has not yet been satisfied?
//
// The first is a *must* analysis and the direction of its uncertainty is what makes the port
// correct. Anything it cannot prove answers false, so an unrecognised construct is treated as not
// calling `super()`, and the rule reports. For a correctness rule that is the right bias, and it is
// also what upstream does: its `Maybe` states resolve toward reporting.
//
// Four constructs carry the whole judgment and each one is in the corpus twice, once in each
// direction:
//
//	if       calls only when it has an else and both arms call
//	try      calls when the finally calls, or when the try calls and there is no catch
//	loops    never count, because the body may run zero times
//	logical  `&&=`, `||=`, `??=` never count, because they short-circuit
//
// The loop rule and the try rule are the two that look like limitations and are not. Upstream fails
// `while (foo) { super(); } this.a();` for exactly the reason given here, and passes
// `try { super(); } finally {} this.a();` while failing
// `try { super(); } catch (err) { } this.a;`, because a catch can swallow a throw from `super()`
// itself and leave `this` unbound.
//
// # What counts as a violation, verified against the corpus rather than assumed
//
// Three questions had to be answered before writing this and all three are pinned by pass cases.
//
// A `this` inside a *nested* function, arrow, or class does not count. Five pass cases say so, and
// they include an arrow, which is the one that looks like it should differ: an arrow does inherit
// the enclosing `this` binding lexically, so `() => this.d()` really does read the constructor's
// `this`. It is still clean, because the arrow *body* does not run at the point it is written. The
// distinction the rule draws is about evaluation time, not about binding, which is why the arrow
// and the function expression land in the same place for different reasons.
//
// A class field initializer does not count, in either direction. Four pass cases, including
// `class C extends B { field = this.foo(); constructor() { } }`, where the initializer is never
// evaluated at all because the constructor never calls `super()`.
//
// `super.method()` counts exactly like `this`. `super.c()` before `super()` is a fail case, because
// the home-object lookup that `super.x` performs also requires the `this` binding.
//
// And `super()` in callee position is not itself a violation, which is why the check asks whether
// the `super` keyword is the callee of its parent call rather than simply whether a `super` token
// appeared.
var NoThisBeforeSuper = rule.Rule[rule.NoOptions]{
	Name: "no-this-before-super",
	Run: func(ruleContext rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners {
		check := func(constructor *ast.Node) {
			if !isDerivedClassConstructor(constructor) {
				return
			}
			declaration := constructor.AsConstructorDeclaration()
			// A constructor overload signature has no body and declares nothing executable.
			//
			// A mutation sweep scored this guard as a survivor, so it was measured rather than
			// kept on faith: with the guard disabled, the overload fixture below does not panic
			// and reports nothing, because `scanStatements` already answers false for a nil node
			// and the shim tolerates `AsNode()` on a nil body. The guard is kept anyway, and the
			// survival is recorded rather than papered over with a fixture that would assert
			// nothing new. Deleting it would make correctness rest on a nil-receiver behaviour
			// this rule does not own, for a saving of one comparison per constructor.
			if declaration.Body == nil {
				return
			}

			scanner := &superScanner{}
			// Parameter defaults evaluate before the body and before `super()`, so a `this` in one
			// is read while the binding does not exist. `constructor(a = this.b) { super(); }` is
			// a real ReferenceError and ESLint reports it; oxc reaches it too, because its walk
			// covers every node in the constructor rather than only the body. Scanning the body
			// alone silently missed this until an invented fixture asked for it.
			violated := false
			if declaration.Parameters != nil {
				for _, parameter := range declaration.Parameters.Nodes {
					if scanner.scanStatements(parameter) {
						violated = true
						break
					}
				}
			}
			if !violated && !scanner.scanStatements(declaration.Body.AsNode()) {
				return
			}
			ruleContext.ReportRange(rule.TokenRange(ruleContext.SourceFile, constructor), rule.Message{
				Id: "thisBeforeSuper",
				Description: "This derived constructor reads `this` or `super` on a path that has " +
					"not called `super()` yet, which throws a ReferenceError because the `this` " +
					"binding does not exist until `super()` returns.",
			})
		}

		return rule.Listeners{
			ast.KindConstructor: check,
		}
	},
}

// isDerivedClassConstructor answers whether this constructor belongs to a class that actually has a
// superclass to call.
//
// A base class has no `super()` to precede, so every `this` in it is fine, and upstream has four
// pass cases pinning that. `extends null` is the sharp one and it is a real language rule rather
// than a curiosity: such a class *cannot* call `super()` without throwing, so its constructor is
// only valid if it never does, and the rule must stay silent. Two pass cases cover it.
func isDerivedClassConstructor(constructor *ast.Node) bool {
	class := constructor.Parent
	if class == nil {
		return false
	}
	if class.Kind != ast.KindClassDeclaration && class.Kind != ast.KindClassExpression {
		return false
	}
	heritage := ast.GetClassExtendsHeritageElement(class)
	if heritage == nil {
		return false
	}
	expression := heritage.Expression()
	if expression == nil {
		return false
	}
	// `extends null` parses as a NullKeyword inside the heritage clause element. Upstream's
	// `!matches!(super_class, Expression::NullLiteral(_))` is the same check.
	return expression.Kind != ast.KindNullKeyword
}

// clauseExits answers whether a statement ends its switch clause.
//
// A `break` leaves the switch and a `return` or `throw` leaves the function, and all three stop
// control falling into the clause below. A labeled `break` targeting an outer statement also
// leaves, and is treated the same, since either way this clause does not fall through.
func clauseExits(statement *ast.Node) bool {
	if statement == nil {
		return false
	}
	if statement.Kind == ast.KindBreakStatement {
		return true
	}
	return bodyDefinitelyExits(statement)
}

// isShortCircuitingAssignment answers whether an assignment operator may skip its right side.
//
// The three logical assignment operators, and nothing else. `||=` and `&&=` skip on the left
// operand's truthiness and `??=` on its nullishness, but all three share the property that matters:
// the right side is not guaranteed to evaluate.
// isShortCircuitingOperator answers whether a binary operator may skip its right operand.
//
// The plain-operator counterparts of the three logical assignments. Separate from them because the
// two appear as different operator tokens on the same node kind, and a single predicate covering
// both would be a list of six with no shared meaning at the call site.
func isShortCircuitingOperator(kind ast.Kind) bool {
	return kind == ast.KindAmpersandAmpersandToken ||
		kind == ast.KindBarBarToken ||
		kind == ast.KindQuestionQuestionToken
}

func isShortCircuitingAssignment(kind ast.Kind) bool {
	return kind == ast.KindAmpersandAmpersandEqualsToken ||
		kind == ast.KindBarBarEqualsToken ||
		kind == ast.KindQuestionQuestionEqualsToken
}

// isSeparateEvaluationContext answers whether a node's contents run at some other time than here.
//
// A function, arrow, class, or accessor written inside a constructor is a value being constructed
// rather than code being run, so the walk stops at it in both directions: a `this` inside does not
// report, and a `super()` inside does not credit.
//
// Arrows are included even though they inherit `this` lexically, because what the rule measures is
// when the body evaluates rather than which binding it would see. Upstream agrees, with
// `var c = () => this.d(); super();` sitting in the pass list next to the function-expression form.
func isSeparateEvaluationContext(node *ast.Node) bool {
	switch node.Kind {
	case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction,
		ast.KindClassDeclaration, ast.KindClassExpression,
		ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor,
		ast.KindConstructor, ast.KindClassStaticBlockDeclaration:
		return true
	}
	return false
}

// isExpressionNode answers whether a node should be routed through the expression walk.
//
// Only the four kinds that walk differently are named. Everything else reaches the generic child
// walk from `scanStatements` and behaves identically either way, so widening this would add cost
// without adding a decision.
func isExpressionNode(node *ast.Node) bool {
	switch node.Kind {
	case ast.KindCallExpression, ast.KindBinaryExpression, ast.KindConditionalExpression,
		ast.KindThisKeyword, ast.KindSuperKeyword:
		return true
	}
	return false
}
