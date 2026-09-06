package core

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/system-inc/cohere/internal/rule"
)

var messagePreserveCaughtError = rule.Message{
	Id: "preserveCaughtError",
	Description: "This throws a new error out of a catch block without attaching the error that " +
		"was caught. The original stack, the original message, and whatever the underlying " +
		"library said about what actually went wrong are all discarded at this line, and what " +
		"reaches the log is a sentence somebody wrote by hand months ago. The failure that " +
		"matters is the one you can no longer see. Pass the caught error as the `cause` option " +
		"so both halves survive.",
}

var messageMissingCatchParameter = rule.Message{
	Id: "missingCatchParameter",
	Description: "This catch clause declares no parameter, so the error it caught cannot be " +
		"reached at all. Nothing in the block can log it, inspect it, or re-attach it to " +
		"whatever gets thrown next, and the information is gone before the first statement " +
		"runs. Name the parameter so the caught error stays available.",
}

// PreserveCaughtErrorOptions configures whether a bare `catch {}` is itself a finding.
//
// The authoritative surface is oxc's `PreserveCaughtErrorOptions`, which derives `JsonSchema` under
// `serde(rename_all = "camelCase", default, deny_unknown_fields)` and carries exactly one field.
// The rule inventory records `options: "no"` for this rule and that is wrong: the option exists,
// upstream's own corpus configures it in both directions, and one of the thirty failing cases
// reports the second message only because it is set.
//
// The default is `false`, which is the Go zero value, so no inversion is needed here. That is
// checked rather than assumed: the derive is `#[derive(Debug, Default, ...)]` on a struct whose
// only field is a plain `bool`, and upstream's corpus carries a passing case with
// `requireCatchParameter: false` alongside a failing case with `true` on the same input. The
// zero-value fixture below pins it.
type PreserveCaughtErrorOptions struct {
	// RequireCatchParameter makes a catch clause with no parameter a finding in its own right.
	//
	// Note what it does NOT do. When it fires, the throw statements inside that catch block are not
	// examined at all: upstream's `check_catch_clause` is an `if let ... else if`, so a parameterless
	// catch either walks its body (impossible, there is no parameter to compare against) or reports
	// this one finding about the clause. So the option does not add a second finding to an input
	// that already reports; it turns silence into exactly one finding, on the clause rather than on
	// the throw.
	RequireCatchParameter bool `json:"requireCatchParameter"`
}

// PreserveCaughtError flags a `throw new Error(...)` inside a catch block that does not attach the
// caught error as `{ cause: err }`.
//
//	valid:   try { a() } catch (err) { throw new Error("m", { cause: err }) }
//	valid:   try { a() } catch (err) { throw new Error("m", { cause: err, extra: 42 }) }
//	valid:   try { a() } catch (err) { throw new Error("m", { ...opts }) }      // spread may carry it
//	valid:   try { a() } catch (err) { throw new Error(...args) }               // spread argument
//	valid:   try { a() } catch (err) { throw new RangeError("m") }              // not one of the three
//	valid:   import { Error } from "./mine"; try { a() } catch (e) { throw Error("m") }
//	valid:   try { a() } catch (e) { o = { bar() { throw new Error() } } }      // a nested function
//	invalid: try { a() } catch (err) { throw new Error("m") }
//	invalid: try { a() } catch (err) { throw new Error("m", { cause: other }) }
//	invalid: try { a() } catch (err) { throw new Error("m", { cause: err.message }) }
//	invalid: try { a() } catch { throw new Error("m") }                         // under the option
//
// # The three constructors, and why the list is short
//
// oxc recognizes exactly `Error`, `TypeError` (options at argument 1) and `AggregateError` (options
// at argument 2, because its first argument is the errors array). Nothing else. That is narrower
// than it looks like it should be: `RangeError`, `SyntaxError`, `EvalError`, `ReferenceError` and
// `URIError` all accept a `cause` option and all are silent here.
//
// ESLint's implementation of the same rule checks eight built-in types and additionally takes an
// `errorClassNames` option for custom ones. Measured on the release binary rather than reasoned
// about: `throw new RangeError("m")` in a catch block is silent under oxlint and reports under
// ESLint. oxc is what the differential harness compares against, so the short list is what ships,
// and the divergence is recorded here rather than quietly improved on.
//
// # Global reference, not name matching
//
// The identifier has to resolve to the global. `import { Error } from "./my-custom-error.js"` puts
// a local `Error` in scope whose constructor signature nobody here knows, and upstream's own corpus
// carries that exact input as a passing case. This asks the checker where the name is declared: a
// global is declared in the TypeScript standard library, which is a declaration file, and anything
// declared in source is a shadow. `resolvesToAGlobal` in this package already answers that question
// for `no-new-native-nonconstructor` and is reused rather than duplicated.
//
// # Two deliberate non-descents, and one that upstream does NOT make
//
// oxc's walker stubs `visit_function` and `visit_catch_clause` to empty bodies, which are two
// independent guards holding two different behaviors:
//
//	a nested function     the caught error is not what that function is throwing about
//	a nested catch        it has its own caught error and is analyzed on its own try statement
//
// Both are reproduced. But `visit_function` covers oxc's `Function` node, and an arrow function is
// a separate node kind with its own visitor that upstream never stubbed. So an arrow inside a catch
// block IS descended into and its throws DO report, while a `function` expression, a function
// declaration, an object method and a class method are all silent. That asymmetry is upstream's
// and it is almost certainly a bug rather than a decision, but it is measured rather than guessed:
// on the release binary, `catch (err) { const f = () => { throw new Error("m"); }; f(); }` reports
// and `catch (err) { const f = function () { throw new Error("m"); }; f(); }` does not. Reproduced,
// because the harness compares against oxlint and improving on it here would read as a difference.
//
// # Symbol identity, not name matching, on the cause value
//
// `{ cause: err }` is only correct when that `err` is the caught one. Upstream resolves the
// identifier to a `symbol_id` and compares it against the catch binding's own symbol, so a
// same-named binding that shadows the parameter is a finding rather than a pass. Upstream's corpus
// carries that input directly:
//
//	catch (error) { if (whatever) { const error = anotherError; throw new Error("m", { cause: error }); } }
//
// Name matching calls that clean. Symbol comparison reports it, which is why this rule declares the
// checker. Both directions are pinned by fixtures below.
//
// The comparison is on the symbol pointer rather than through `symbol.Declarations[0]`, because the
// question here is "are these two identifiers the same binding" rather than "which declaration is
// this name". A catch parameter has exactly one declaration and cannot merge, so the two spellings
// agree on this rule's inputs; the pointer comparison is used because it answers the question
// asked, not because indexing would have been wrong here.
//
// # No parenthesis skipping, in either position, and both were measured
//
// Upstream destructures `Expression::Identifier` directly at the callee and at the cause value, so
// a parenthesis defeats each one. Measured on the release binary: `throw new (Error)("m")` in a
// catch block is **silent**, and `throw new Error("m", { cause: (err) })` **reports**. Two positions,
// opposite-looking outcomes, one cause. Adding a paren skip at either site would flip a real
// verdict, so neither is added and this paragraph is why.
//
// # Which `cause` wins when there are two
//
// Upstream's scan returns on the FIRST property whose key is the identifier `cause`, so a later one
// is never read. Measured: `{ cause: err, cause: other }` is **silent** and `{ cause: other, cause: err }`
// **reports**, which is the opposite of what JavaScript does at runtime, where the last one wins.
// ESLint takes the last (`causeProperties.at(-1)`) and therefore disagrees on both inputs. oxc's
// answer is reproduced.
//
// The key must be a plain identifier. A string-literal key is not read as `cause` at all:
// `{ "cause": err }` **reports** on the release binary, where ESLint's `getStaticPropertyName`
// resolves it and stays clean. Reproduced, and this is also why the fix declines that shape (below).
//
// # What is exempt
//
//	a spread argument       `throw new Error(...args)` — the arguments cannot be counted
//	a spread property       `{ ...opts }` — the cause may be in there
//	a destructured param    `catch ({ message })` has no single name to compare or to write
//
// The last is worth naming because ESLint treats it as a distinct finding — `partiallyLostError`,
// reported on the catch clause. oxc has no such message: a destructured parameter still walks the
// body, every throw is still checked, and `is_catch_parameter` simply answers false for every value
// because the binding is not an identifier. So the throw reports with the ordinary message and the
// clause reports nothing. Upstream's corpus carries `catch ({ message }) { throw new Error(message) }`
// as a failing case with one diagnostic, which is what settles it.
//
// # The repair, and the two shapes it declines
//
// Five fix shapes ship, each measured against the release binary rather than read off the source:
//
//	no arguments            `new Error()`           → `new Error("", { cause: err })`
//	no arguments, aggregate `new AggregateError()`  → `new AggregateError([], "", { cause: err })`
//	arguments short of the  `new Error("m")`        → `new Error("m", { cause: err })`
//	  options slot          `new AggregateError([])`→ `new AggregateError([], "", { cause: err })`
//	an empty options object `{}`                    → `{ cause: err }`
//	options with properties `{ a: 1 }`              → `{ a: 1, cause: err }`
//	a wrong `cause` value   `{ cause: other }`      → `{ cause: err }`
//
// Two shapes report with NO repair, and that is a deliberate narrowing of upstream rather than a
// gap. Both were measured by running `oxlint --fix` and reading the file it wrote:
//
//	{ "cause": err }              upstream writes `{ "cause": err, cause: err }`
//	{ cause: other, cause: err }  upstream writes `{ cause: err, cause: err }`
//
// Both outputs carry a duplicate `cause` key. That is a `no-dupe-keys` violation upstream itself
// reports, and it changes what the code does, since the last key wins at runtime and the first one
// is what this rule was reading. A fix is applied unattended, so shipping one that writes a
// duplicate key into Kirk's tree is worse than shipping no fix for those two inputs. They still
// report; they simply carry no repair, which is the subset this port can show correct.
//
// Upstream also proposes no fix for several inputs it reports, and those are reproduced as-is:
// an options argument that is not an object literal (`new Error("m", o)`, `new Error("m", 5)`),
// more arguments than the constructor takes (`new Error("m", {}, 3)`), and an `AggregateError`
// whose third argument is not an object. All four were measured to report-without-fixing.
//
// The repair is a fix rather than a suggestion because attaching a cause cannot change what the
// surrounding code means: the thrown value is the same error with one more property set, and there
// is no second valid answer to choose between.
var PreserveCaughtError = rule.Rule{
	Name: "preserve-caught-error",

	// See the doc above: the discriminations are which binding a `cause` value names, and whether
	// `Error` is the global or a local shadowing it. Both are symbol questions.
	NeedsTypeChecker: true,

	Run: func(ctx rule.Context, options any) rule.Listeners {
		// A rule configured as bare `"error"` is handed nil options rather than a zero struct, so
		// the assertion below fails and leaves the zero value. That is the correct default here
		// (upstream's `requireCatchParameter` defaults to false), but it is written as an explicit
		// two-value assertion rather than a bare one so that the nil path is visible at the site
		// rather than implied.
		parsed, _ := options.(PreserveCaughtErrorOptions)

		return rule.Listeners{
			ast.KindCatchClause: func(node *ast.Node) {
				// Reporting on a nil checker would mean reporting every `throw new Error` in every
				// catch block in the tree, since neither the global test nor the cause comparison
				// can answer without one. Silence is the only safe direction.
				if ctx.TypeChecker == nil {
					return
				}

				clause := node.AsCatchClause()
				if clause.Block == nil {
					return
				}

				binding := catchClauseBinding(clause)
				if binding == nil {
					// Upstream's `else if`: a parameterless catch either reports this one finding
					// or nothing at all, and never walks its body. The throws inside it are not
					// examined even when the option is on, because there is no caught error to
					// compare a cause against.
					if parsed.RequireCatchParameter {
						ctx.ReportNode(node, messageMissingCatchParameter)
					}
					return
				}

				// The binding may be a destructuring pattern, in which case there is no identifier
				// to compare a cause value against and no name to write into a repair. Upstream
				// still walks the body and still reports every throw; `is_catch_parameter` just
				// answers false for everything. `bindingIdentifier` is nil in that case and the
				// code below treats it as "nothing can match, and no fix can be built".
				bindingIdentifier := binding
				if bindingIdentifier.Kind != ast.KindIdentifier {
					bindingIdentifier = nil
				}

				var catchSymbol *ast.Symbol
				if bindingIdentifier != nil {
					catchSymbol = ctx.TypeChecker.GetSymbolAtLocation(bindingIdentifier)
				}

				visitCatchBodyForThrows(ctx, clause.Block, func(throwStatement *ast.Node) {
					checkThrownError(ctx, throwStatement, bindingIdentifier, catchSymbol)
				})
			},
		}
	},
}

// catchClauseBinding returns the name node a catch clause binds, or nil for a bare `catch {}`.
//
// TypeScript spells the catch parameter as a `VariableDeclaration` hanging off the clause rather
// than as a parameter, so the name lives one level down. A bare `catch {}` has no declaration at
// all, which is the case the option above turns into a finding.
func catchClauseBinding(clause *ast.CatchClause) *ast.Node {
	if clause.VariableDeclaration == nil {
		return nil
	}
	return clause.VariableDeclaration.Name()
}

// visitCatchBodyForThrows walks a catch block's statements and calls back on every throw statement
// that upstream's walker would reach.
//
// The two stubs upstream writes are reproduced here as two separate early returns, deliberately not
// merged, because they hold two independent behaviors and a mutation sweep has to be able to tell
// them apart. See the rule doc for why an arrow function is NOT one of them.
func visitCatchBodyForThrows(ctx rule.Context, node *ast.Node, onThrow func(*ast.Node)) {
	if node == nil {
		return
	}

	switch node.Kind {
	case ast.KindThrowStatement:
		onThrow(node)
		// A throw's argument can contain another throw only inside a function expression, which
		// the guard below already declines, so there is nothing further to walk here. Falling
		// through to the children would be harmless and is omitted only because upstream's
		// visitor does the same.
		return

	case ast.KindCatchClause:
		// A nested catch has its own caught error and is analyzed when the walk reaches its own
		// try statement. Descending here would compare its throws against the OUTER catch's
		// parameter, so `catch (a) { try {} catch (b) { throw new Error("m", { cause: b }) } }`
		// would report despite being upstream's own passing case.
		return

	case ast.KindFunctionDeclaration, ast.KindFunctionExpression,
		ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor,
		ast.KindConstructor:
		// oxc's `visit_function` stub. A throw inside a function defined in the catch block runs
		// whenever that function is called, which is not necessarily while the caught error is
		// meaningful. Upstream's corpus pins this with `foo = { bar() { throw new Error(); } }`
		// as a PASSING case.
		//
		// An arrow function is deliberately absent from this list. See the rule doc: oxc's
		// stub covers its `Function` node only, arrows have their own unstubbed visitor, and
		// the release binary reports through them. Adding `ast.KindArrowFunction` here would
		// silence four inputs oxlint reports.
		return
	}

	node.ForEachChild(func(child *ast.Node) bool {
		visitCatchBodyForThrows(ctx, child, onThrow)
		return false
	})
}

// errorOptionsArgumentIndex reports which argument position carries the error options object, and
// whether the callee is a constructor this rule recognizes at all.
//
// The second return is the "is this ours" answer and is why this is one function rather than two:
// upstream's `error_options_argument_index` returns an `Option<usize>`, where `None` means both
// "unrecognized name" and "not the global", and every caller treats them identically.
func errorOptionsArgumentIndex(ctx rule.Context, callee *ast.Node) (int, bool) {
	// No parenthesis skip. `new (Error)("m")` is silent on the release binary because upstream
	// destructures the identifier directly, and reproducing that is the whole reason this is a
	// kind test rather than a call into a skipping accessor.
	if callee == nil || callee.Kind != ast.KindIdentifier {
		return 0, false
	}

	name := callee.Text()
	if name != "Error" && name != "TypeError" && name != "AggregateError" {
		return 0, false
	}

	// A local binding of the same name shadows the global and its signature is unknown, so the
	// options position cannot be assumed. Upstream's corpus carries the import form as a passing
	// case.
	if !resolvesToAGlobal(ctx, callee) {
		return 0, false
	}

	if name == "AggregateError" {
		// AggregateError's first argument is the errors iterable, so the message is second and the
		// options object is third.
		return 2, true
	}
	return 1, true
}

// checkThrownError decides whether one throw statement inside a catch block reports, and builds the
// repair when one can be shown correct.
func checkThrownError(ctx rule.Context, throwStatement *ast.Node, bindingIdentifier *ast.Node, catchSymbol *ast.Symbol) {
	thrown := throwStatement.AsThrowStatement().Expression
	if thrown == nil {
		return
	}

	var callee *ast.Node
	var arguments []*ast.Node
	var typeArguments *ast.NodeList

	switch thrown.Kind {
	case ast.KindNewExpression:
		expression := thrown.AsNewExpression()
		callee = expression.Expression
		typeArguments = expression.TypeArguments
		if expression.Arguments != nil {
			arguments = expression.Arguments.Nodes
		}
	case ast.KindCallExpression:
		expression := thrown.AsCallExpression()
		callee = expression.Expression
		typeArguments = expression.TypeArguments
		if expression.Arguments != nil {
			arguments = expression.Arguments.Nodes
		}
	default:
		// `throw err`, `throw "string"`, `throw foo()` where foo is not an error constructor.
		return
	}

	optionsIndex, recognized := errorOptionsArgumentIndex(ctx, callee)
	if !recognized {
		return
	}

	// An options object already carrying the right cause is the clean case, and it is checked
	// before the spread test because upstream checks it first.
	if optionsIndex < len(arguments) && arguments[optionsIndex].Kind == ast.KindObjectLiteralExpression {
		if objectHasCorrectCause(ctx, arguments[optionsIndex], catchSymbol) {
			return
		}
	}

	// A spread argument makes the positions unknowable: `new Error(...args)` may already be
	// supplying an options object with a cause in it. Upstream declines the whole throw rather
	// than guessing, and its corpus carries that input as a passing case.
	for _, argument := range arguments {
		if argument.Kind == ast.KindSpreadElement {
			return
		}
	}

	fixes := buildCauseFix(ctx, thrown, callee, typeArguments, arguments, optionsIndex, bindingIdentifier, catchSymbol)
	if len(fixes) == 0 {
		ctx.ReportNode(throwStatement, messagePreserveCaughtError)
		return
	}
	ctx.ReportNodeWithFixes(throwStatement, messagePreserveCaughtError, fixes...)
}

// objectHasCorrectCause reports whether an options object already attaches the caught error.
//
// It answers true in two shapes, and the second is not an approximation of the first. A spread
// property may expand to a `cause` nobody here can see, so upstream returns true on the first one
// it meets and stops. A `cause` key whose value is the caught binding is the ordinary clean case.
//
// The scan returns on the FIRST `cause` key rather than the last, which is upstream's structure and
// is measured to matter: `{ cause: err, cause: other }` is silent and `{ cause: other, cause: err }`
// reports. JavaScript's own answer is the opposite, and ESLint's is too.
func objectHasCorrectCause(ctx rule.Context, object *ast.Node, catchSymbol *ast.Symbol) bool {
	for _, property := range object.AsObjectLiteralExpression().Properties.Nodes {
		if property.Kind == ast.KindSpreadAssignment {
			return true
		}
		key := causePropertyKeyName(property)
		if key != "cause" {
			continue
		}
		return isCatchParameterValue(ctx, causePropertyValue(property), catchSymbol)
	}
	return false
}

// causePropertyKeyName returns a property's key text, but ONLY when the key is a plain identifier.
//
// This is deliberately narrower than `ast.TryGetTextOfPropertyName`, which is the shelf helper a
// reader will reach for and which resolves a string-literal key, a numeric key, and a computed key
// holding a literal. Upstream matches `PropertyKey::StaticIdentifier` and nothing else, so
// `{ "cause": err }` is not read as a cause at all and reports on the release binary. Using the
// broader helper would silence that input, which is a real divergence rather than a refinement.
//
// The narrowness also protects the fix: because a string-literal key is invisible here, a repair
// that appended `cause: err` to such an object would produce a duplicate key. That fix is declined
// for exactly this reason, and the two decisions are kept in step by both reading this function.
func causePropertyKeyName(property *ast.Node) string {
	name := property.Name()
	if name == nil || name.Kind != ast.KindIdentifier {
		return ""
	}
	return name.Text()
}

// causePropertyValue returns the value expression of an object literal member, or nil where the
// member has no value expression of its own.
//
// A shorthand (`{ cause }`), a method (`{ cause() {} }`) and an accessor (`{ get cause() {} }`) all
// return nil, which makes `isCatchParameterValue` answer false for each. That matches upstream,
// where every one of those falls out of the `Expression::Identifier` destructure, and all three are
// failing cases in upstream's corpus.
//
// This package already has a `propertyValue`, in no_self_assign.go, and it is deliberately NOT
// reused. That one resolves a shorthand to the name node, which is the right answer for a
// self-assignment comparison and the wrong one here: it would make `{ cause }` resolve to an
// identifier spelled `cause`, and the symbol comparison below would then answer on whatever
// `cause` happens to be in scope. Upstream reports `{ cause }` unconditionally, and its corpus
// carries that input as a failing case with a fix that rewrites the whole property. Reusing the
// shelf function here would have silenced it wherever an unrelated `cause` binding existed.
func causePropertyValue(property *ast.Node) *ast.Node {
	if property.Kind != ast.KindPropertyAssignment {
		return nil
	}
	return property.AsPropertyAssignment().Initializer
}

// isCatchParameterValue reports whether an expression is exactly the caught error binding.
//
// No parenthesis skip and no unwrapping of any kind. `{ cause: (err) }` and `{ cause: err! }` both
// report on the release binary because upstream destructures `Expression::Identifier` directly, and
// this reproduces that by testing the kind rather than reaching through an accessor that skips.
//
// The comparison is symbol identity rather than text, which is the whole reason this rule declares
// the checker. `catch (error) { if (w) { const error = other; throw new Error("m", { cause: error }) } }`
// is a failing case in upstream's corpus and is clean under any name comparison.
func isCatchParameterValue(ctx rule.Context, value *ast.Node, catchSymbol *ast.Symbol) bool {
	if value == nil || catchSymbol == nil {
		return false
	}
	if value.Kind != ast.KindIdentifier {
		return false
	}
	return ctx.TypeChecker.GetSymbolAtLocation(value) == catchSymbol
}

// buildCauseFix assembles the repair for one reporting throw, or returns nil where no repair can be
// shown correct.
//
// Every shape here was measured by running `oxlint --fix` on the input and reading the file it
// wrote, rather than by reading the fixer source. Two shapes upstream repairs are deliberately not
// repaired here because upstream's output carries a duplicate `cause` key; see the rule doc.
func buildCauseFix(
	ctx rule.Context,
	thrown *ast.Node,
	callee *ast.Node,
	typeArguments *ast.NodeList,
	arguments []*ast.Node,
	optionsIndex int,
	bindingIdentifier *ast.Node,
	catchSymbol *ast.Symbol,
) []rule.Fix {
	// A destructured catch parameter has no name to write, so upstream returns `fixer.noop()` and
	// so does this. The finding still lands; only the repair is withheld.
	if bindingIdentifier == nil {
		return nil
	}
	causeText := "cause: " + bindingIdentifier.Text()

	isAggregate := callee.Kind == ast.KindIdentifier && callee.Text() == "AggregateError"

	switch {
	case len(arguments) == 0:
		// The insertion point is just inside the opening parenthesis of the argument list, which is
		// NOT simply the character after the callee. Two of upstream's own cases exist to say so:
		// `new Error/* ( */()` has a parenthesis inside a comment and `new Error<() => void>()` has
		// two inside the type arguments. Starting the scan after the type arguments where they
		// exist, and after the callee otherwise, is what steps past both.
		scanFrom := callee.End()
		if typeArguments != nil {
			scanFrom = typeArguments.End()
		}
		openParenthesis := findOpeningParenthesis(ctx, scanFrom, thrown.End())
		if openParenthesis < 0 {
			// `throw new Error` with no argument list at all is valid JavaScript and there is no
			// parenthesis to insert after. Upstream reaches `fixer.noop()` here for the same
			// reason.
			return nil
		}
		inserted := "\"\", { " + causeText + " }"
		if isAggregate {
			inserted = "[], " + inserted
		}
		return []rule.Fix{rule.ReplaceRange(
			core.NewTextRange(openParenthesis+1, openParenthesis+1), inserted)}

	case len(arguments) <= optionsIndex:
		// Arguments are present but stop short of the options slot, so any missing positional
		// argument between the last one written and the options slot is synthesized as an empty
		// string before it. For a plain Error, where options are at index 1, the loop body never
		// runs and this is the plain `new Error("m")` append. For AggregateError, where options
		// are at index 2, `new AggregateError([])` needs a message inserted first, which is the
		// case the loop exists for and which upstream's own fix vectors assert.
		last := arguments[len(arguments)-1]
		inserted := ", { " + causeText + " }"
		for position := len(arguments); position < optionsIndex; position++ {
			inserted = ", \"\"" + inserted
		}
		return []rule.Fix{rule.ReplaceRange(
			core.NewTextRange(last.End(), last.End()), inserted)}
	}

	optionsArgument := arguments[optionsIndex]
	if optionsArgument.Kind != ast.KindObjectLiteralExpression {
		// `new Error("m", o)` and `new Error("m", 5)`. Upstream reports these and proposes nothing,
		// because it cannot see inside a value it does not own. Measured on the release binary:
		// both report and `--fix` leaves the file unchanged.
		return nil
	}

	// More arguments than the constructor takes. `new Error("m", {}, 3)` reports with no repair
	// upstream, and this reproduces that rather than editing an object whose role is unclear.
	if len(arguments) > optionsIndex+1 {
		return nil
	}

	properties := optionsArgument.AsObjectLiteralExpression().Properties.Nodes

	if len(properties) == 0 {
		// `{}` → `{ cause: err }`. The insertion goes inside the braces, so the range is the point
		// just before the closing brace rather than the object's own end.
		return []rule.Fix{rule.ReplaceRange(
			core.NewTextRange(optionsArgument.End()-1, optionsArgument.End()-1), " "+causeText+" ")}
	}

	// An existing `cause` key. The first one is the one upstream reads, so it is the one replaced.
	for index, property := range properties {
		if causePropertyKeyName(property) != "cause" {
			continue
		}

		// A later `cause` key means the repair would leave two of them in the object, which is
		// what upstream writes and what this declines. Measured: upstream turns
		// `{ cause: other, cause: err }` into `{ cause: err, cause: err }`.
		for _, later := range properties[index+1:] {
			if causePropertyKeyName(later) == "cause" {
				return nil
			}
		}

		value := causePropertyValue(property)
		if value == nil {
			// Shorthand, method, or accessor. Upstream replaces the whole property rather than a
			// value it does not have, and its corpus asserts exactly that for all three shapes:
			// `{ cause }`, `{ cause() {} }`, `{ get cause() {} }` all become `{ cause: err }`.
			return []rule.Fix{ctx.ReplaceNode(property, causeText)}
		}
		return []rule.Fix{ctx.ReplaceNode(value, bindingIdentifier.Text())}
	}

	// No `cause` key that this rule can see. A string-literal key spelled "cause" is invisible to
	// `causePropertyKeyName` by design, and appending here would produce a duplicate key that
	// changes what the object means at runtime. Upstream writes that duplicate; this declines.
	for _, property := range properties {
		if isStringLiteralCauseKey(property) {
			return nil
		}
	}

	// `{ a: 1 }` → `{ a: 1, cause: err }`. A computed key is deliberately not a reason to decline:
	// upstream appends past it on the reasoning that a computed key cannot be proven to be
	// `cause`, and its corpus asserts `{ [cause]: "Some error" }` becoming
	// `{ [cause]: "Some error", cause: error }`.
	last := properties[len(properties)-1]
	return []rule.Fix{rule.ReplaceRange(
		core.NewTextRange(last.End(), last.End()), ", "+causeText)}
}

// isStringLiteralCauseKey reports whether a property's key is the string literal "cause".
//
// It exists only so the fix can decline that shape. The rule's own reading deliberately does not
// recognize such a key, matching upstream, so an object carrying one still REPORTS; what this
// prevents is the repair writing a second `cause` beside it.
func isStringLiteralCauseKey(property *ast.Node) bool {
	name := property.Name()
	if name == nil {
		return false
	}
	if name.Kind != ast.KindStringLiteral && name.Kind != ast.KindNoSubstitutionTemplateLiteral {
		return false
	}
	return name.Text() == "cause"
}

// findOpeningParenthesis returns the offset of the argument list's opening parenthesis, scanning
// forward from a position the caller has already stepped past the callee and its type arguments.
//
// A plain search for the next `(` in the source text would be wrong, and upstream carries two
// corpus cases proving it: `new Error/* ( */()` puts one inside a comment and
// `new Error<() => void>()` puts two inside the type arguments. The type-argument half is handled
// by the caller's scan start; the comment half is handled here by scanning tokens rather than
// characters.
func findOpeningParenthesis(ctx rule.Context, from int, to int) int {
	text := ctx.SourceFile.Text()
	if from < 0 || to > len(text) || from >= to {
		return -1
	}

	position := from
	for position < to {
		switch {
		case text[position] == '(':
			return position
		case text[position] == '/' && position+1 < to && text[position+1] == '/':
			for position < to && text[position] != '\n' {
				position++
			}
		case text[position] == '/' && position+1 < to && text[position+1] == '*':
			position += 2
			for position+1 < to && !(text[position] == '*' && text[position+1] == '/') {
				position++
			}
			position += 2
		default:
			position++
		}
	}
	return -1
}
