package core

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/system-inc/cohere/internal/lint/ecmascript/control_flow_graph"
	"github.com/system-inc/cohere/internal/lint/ecmascript/property"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// ConsistentReturnSettings is the decoded option surface.
//
// One boolean, defaulting to false, which is the zero value. Unlike no-useless-computed-key the
// generic decoder would answer correctly here, and it is still hand rolled so that an explicit
// `false` and an absent key stay distinguishable in the wire type rather than by accident.
type ConsistentReturnSettings struct {
	// TreatUndefinedAsUnspecified makes `return undefined` and `return void 0` count as returning
	// nothing, so a function mixing them with a bare `return` is consistent.
	TreatUndefinedAsUnspecified bool
}

// DefaultConsistentReturnSettings is upstream's `defaultOptions: [{treatUndefinedAsUnspecified: false}]`.
func DefaultConsistentReturnSettings() ConsistentReturnSettings {
	return ConsistentReturnSettings{TreatUndefinedAsUnspecified: false}
}

type consistentReturnWire struct {
	TreatUndefinedAsUnspecified *bool `json:"treatUndefinedAsUnspecified"`
}

// DecodeConsistentReturnOptions reads the option object off the config.
func DecodeConsistentReturnOptions(raw []byte) (any, error) {
	if len(raw) == 0 {
		return DefaultConsistentReturnSettings(), nil
	}
	var wire consistentReturnWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		return DefaultConsistentReturnSettings(), err
	}
	settings := DefaultConsistentReturnSettings()
	if wire.TreatUndefinedAsUnspecified != nil {
		settings.TreatUndefinedAsUnspecified = *wire.TreatUndefinedAsUnspecified
	}
	return settings, nil
}

var messageConsistentReturnMissingReturn = rule.Message{
	Id: "missingReturn",
	Description: "Some paths through this function return a value and some run off the end, which " +
		"returns undefined. A caller reading one branch cannot tell which kind of function this " +
		"is, so the undefined arrives somewhere far from here.",
}

var messageConsistentReturnMissingReturnValue = rule.Message{
	Id: "missingReturnValue",
	Description: "This returns nothing while another return in the same function returns a value. " +
		"The caller receives undefined from one path and a value from another, and nothing in the " +
		"signature says which.",
}

var messageConsistentReturnUnexpectedReturnValue = rule.Message{
	Id: "unexpectedReturnValue",
	Description: "This returns a value while another return in the same function returns nothing. " +
		"A reader who saw the bare return will not expect a value to come back from here.",
}

// ConsistentReturn requires every `return` in a function to agree about whether it carries a value.
//
//	valid:   function foo() { if (true) return; else return; }
//	valid:   function foo() { if (true) return true; else return false; }
//	invalid: function foo() { if (true) return true; else return; }
//	invalid: function foo() { if (a) return true; }
//
// # Two judgments, and the second is why this needs a control-flow graph
//
// The first is syntactic: within one function, the FIRST `return` sets the expectation and every
// later one that disagrees is reported, at that return statement. The second asks whether a
// function that returns a value anywhere can also run off its end, which is not a syntactic
// question at all. `function foo() { if (a) return true; }` reports and
// `function foo() { if (a) return true; else throw 1; }` does not, and the difference is whether
// any path reaches the closing brace.
//
// Upstream asks `isAnySegmentReachable(funcInfo.currentSegments)` at the function's exit.
// `control_flow_graph.Graph.EndReachable` is documented as answering that exact question and is
// already used for it by `array_callback_return`, so this rule is a caller rather than a
// reimplementation. Measured agreement on six shapes including a trailing `throw`, an
// `if`/`else throw`, and `while (true) { return 1; }`, which upstream and the graph both call
// unreachable.
//
// # The message text is three messages and a rendered name, and the casing differs between them
//
// `missingReturn` renders the name in lower case -- "at the end of function 'foo'" -- while the two
// per-return messages render it capitalised -- "Function 'foo' expected a return value". Upstream
// gets this from `upperCaseFirst` applied at one of the two sites and not the other. The corpus
// asserts both spellings, and the `Program` case asserts "Program" capitalised against "program"
// lower case in the same file, so the two cannot be collapsed.
//
// # Constructors are exempt, and only some of them
//
// A class constructor is skipped because `return` in one is a language feature rather than a value
// channel. An object literal's method NAMED `constructor` is not a constructor and is not exempt:
// measured against the installed rule, `({ constructor() { if (a) return true; } })` reports as
// "method 'constructor'" while `class A { constructor() { if (a) return true; } }` is silent. The
// corpus asserts both.
//
// Upstream also exempts an ES5 constructor, which is `astUtils.isES5Constructor`: a function
// declaration or expression whose name begins with a capital letter. The corpus's
// `function Foo() { if (!(this instanceof Foo)) return new Foo(); }` is clean for that reason and
// for no other, and `class A { CapitalizedFunction() {...} }` REPORTS, which is what shows the
// exemption is about the function's own name rather than about capitalisation anywhere.
//
// # What this does NOT ask
//
// Upstream never consults a type annotation, so `function foo(): number | undefined { if (a)
// return true; }` reports even though the annotation makes the mixed return correct TypeScript.
// Measured against the installed rule. That is the extension rule's job:
// `@typescript-eslint/consistent-return` wraps this one and filters on the declared return type.
// Reproduced as-is rather than improved on, because narrowing here would silently change what the
// extension rule is a wrapper around.
var ConsistentReturn = rule.Rule{
	Name: "consistent-return",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		settings, ok := options.(ConsistentReturnSettings)
		if !ok {
			settings = DefaultConsistentReturnSettings()
		}

		return rule.Listeners{
			// One listener over the whole file rather than one per function kind, because the
			// judgment is about a function as a whole and the walk here is pre-order: a listener
			// on the function cannot see the returns that follow it.
			ast.KindSourceFile: func(node *ast.Node) {
				checkConsistentReturnScope(ctx, node, settings)
			},
		}
	},
}

// checkConsistentReturnScope judges one function-like scope and descends into those below it.
func checkConsistentReturnScope(ctx rule.Context, scope *ast.Node, settings ConsistentReturnSettings) {
	var returns []*ast.Node
	var nested []*ast.Node

	var visit func(*ast.Node) bool
	visit = func(current *ast.Node) bool {
		if isConsistentReturnScope(current) {
			// A nested function owns its own returns and is judged on its own pass. This is what
			// upstream's code path stack does when it pushes a frame.
			nested = append(nested, current)
			return false
		}
		if current.Kind == ast.KindReturnStatement {
			returns = append(returns, current)
		}
		current.ForEachChild(visit)
		return false
	}
	// A class member's body is reached through the class, so the descent must not stop at a class
	// declaration; only a function-like node opens a new frame.
	scope.ForEachChild(visit)

	judgeConsistentReturns(ctx, scope, returns, settings)

	for _, function := range nested {
		checkConsistentReturnScope(ctx, function, settings)
	}
}

// judgeConsistentReturns runs both judgments over one function's own returns.
func judgeConsistentReturns(ctx rule.Context, scope *ast.Node, returns []*ast.Node,
	settings ConsistentReturnSettings) {

	if len(returns) == 0 {
		return
	}

	// A constructor's `return` is a language feature rather than a value channel, so upstream skips
	// both judgments for one. Checked before anything is reported rather than only at the
	// end-of-function judgment, because upstream's ReturnStatement listener also runs inside a
	// constructor and its first return simply sets an expectation nothing later contradicts... and
	// measured, `class A { constructor() { if (a) return true; else return; } }` DOES report the
	// per-return finding upstream. So only the end-of-function judgment is exempt, which is where
	// upstream's isClassConstructor test actually sits.
	firstReturnHasValue := consistentReturnHasValue(returns[0], settings)
	expectationMessage := messageConsistentReturnUnexpectedReturnValue
	if firstReturnHasValue {
		expectationMessage = messageConsistentReturnMissingReturnValue
	}
	capitalisedName := consistentReturnName(scope, true)

	anyReturnHasValue := firstReturnHasValue
	for _, returnStatement := range returns[1:] {
		hasValue := consistentReturnHasValue(returnStatement, settings)
		if hasValue {
			anyReturnHasValue = true
		}
		if hasValue == firstReturnHasValue {
			continue
		}
		ctx.ReportNode(returnStatement, rule.Message{
			Id: expectationMessage.Id,
			Description: fmt.Sprintf("%s %s %s",
				capitalisedName,
				consistentReturnVerb(expectationMessage.Id),
				expectationMessage.Description),
		})
	}

	// The end-of-function judgment. Upstream reads `funcInfo.hasReturnValue`, which is set from the
	// FIRST return alone and never updated, so a function whose first return is bare and whose
	// later return carries a value is NOT reported here even though a value does escape it. That
	// looks like a defect and it is upstream's behaviour; its own corpus case
	// `function foo() { if (true) return; else return false; }` reports only the per-return
	// finding. Reproduced by reading the first return rather than any return.
	_ = anyReturnHasValue
	if !firstReturnHasValue {
		return
	}
	if isConsistentReturnExemptFromEndJudgment(scope) {
		return
	}
	if !consistentReturnCanRunOffEnd(scope) {
		return
	}

	reportRange := consistentReturnReportRange(ctx, scope)
	ctx.ReportRange(reportRange, rule.Message{
		Id: messageConsistentReturnMissingReturn.Id,
		Description: fmt.Sprintf("Expected to return a value at the end of %s. %s",
			consistentReturnName(scope, false), messageConsistentReturnMissingReturn.Description),
	})
}

// consistentReturnVerb renders the middle of the two per-return messages.
//
// Upstream spells these as `{{name}} expected a return value.` and
// `{{name}} expected no return value.`, so the verb differs with the message rather than with the
// name and there is exactly one of each.
func consistentReturnVerb(messageId string) string {
	if messageId == messageConsistentReturnMissingReturnValue.Id {
		return "expected a return value."
	}
	return "expected no return value."
}

// consistentReturnHasValue says whether a return statement carries a value, under the option.
//
// `treatUndefinedAsUnspecified` makes `return undefined` and `return void 0` count as returning
// nothing. Upstream tests `isSpecificId(argument, "undefined")` and `argument.operator !== "void"`,
// so it matches the bare identifier `undefined` and any `void` expression, not only `void 0`.
func consistentReturnHasValue(returnStatement *ast.Node, settings ConsistentReturnSettings) bool {
	argument := returnStatement.AsReturnStatement().Expression
	if argument == nil {
		return false
	}
	if !settings.TreatUndefinedAsUnspecified {
		return true
	}
	// NOT unwrapping parentheses here, because upstream does not either: its parser folds them away
	// so `return (undefined)` arrives as a bare identifier there and is treated as unspecified,
	// while ours keeps the node and treats it as a value. Recorded as a divergence rather than
	// corrected, because correcting it would be a silent improvement on a rule whose whole option
	// surface is about which spellings count as nothing, and upstream's corpus writes no
	// parenthesized form to settle the intent.
	if argument.Kind == ast.KindVoidExpression {
		return false
	}
	if argument.Kind == ast.KindIdentifier && argument.Text() == "undefined" {
		return false
	}
	return true
}

// isConsistentReturnScope says whether a node owns its own returns.
//
// Every function-like construct, including a constructor and both accessors, because each is a
// separate code path frame upstream. A class declaration is deliberately absent: its members are
// reached by descending through it.
func isConsistentReturnScope(node *ast.Node) bool {
	switch node.Kind {
	case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction,
		ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor,
		ast.KindConstructor:
		return true
	}
	return false
}

// isConsistentReturnExemptFromEndJudgment reproduces upstream's two constructor exemptions.
func isConsistentReturnExemptFromEndJudgment(node *ast.Node) bool {
	// A class constructor. Our parser gives it its own kind where upstream reads a
	// FunctionExpression whose parent is a MethodDefinition of kind "constructor", which is why an
	// object literal's method named `constructor` is NOT caught by this and does report.
	if node.Kind == ast.KindConstructor {
		return true
	}

	// An ES5 constructor: `astUtils.isES5Constructor` is a function whose own name starts with an
	// upper case letter. Upstream tests `node.id && node.id.name[0] !== node.id.name[0].toLowerCase()`,
	// which is a case comparison rather than an ASCII range, so a name with no case distinction at
	// all -- a digit cannot start an identifier, but `_foo` and `$foo` can -- is NOT a constructor.
	// The corpus asserts `function _foo()` reports, which is the case that separates the two
	// readings.
	if node.Kind != ast.KindFunctionDeclaration && node.Kind != ast.KindFunctionExpression {
		return false
	}
	name := node.Name()
	if name == nil {
		return false
	}
	text := name.Text()
	if text == "" {
		return false
	}
	first := text[:1]
	return first != strings.ToLower(first)
}

// consistentReturnCanRunOffEnd reports whether control can reach the end of a function body.
//
// Upstream's `isAnySegmentReachable(funcInfo.currentSegments)` at the function's exit.
// `control_flow_graph.Graph.EndReachable` is documented as answering that question, and
// `array_callback_return` already calls it for the identical purpose.
//
// A nil graph answers true, which is the conservative direction: it reports rather than going
// silent, so a shape the builder cannot model surfaces instead of disappearing.
//
// # A measured over-report, in the shelf rather than in this rule
//
// A dry run over the ahra tree reported 141 findings where the installed rule reports 130. The 130
// agree exactly, same file, same line, same column, same message id, and there are ZERO findings
// the installed rule produces that this one misses. All 11 extra come from one shape, isolated by
// bisection down to a minimal pair:
//
//	try { const r = g(); return r; } catch (e) { throw e; } finally { k(); }   EndReachable TRUE
//	try { return 1; }                catch (e) { throw e; } finally { k(); }   EndReachable false
//	try { const r = g(); return r; } catch (e) { throw e; }                    EndReachable false
//
// So a variable declaration inside a `try` that also has a `finally` makes the graph believe
// control reaches the function's end when every path returns or throws. Removing either the
// declaration or the `finally` gives the right answer, which is what places the defect in
// `control_flow_graph` rather than here: this rule asks one question and the shelf answers it
// wrongly for that one shape.
//
// Not repaired here, and not worked around here either. A workaround would be this rule
// disagreeing with the shelf about reachability, which is worse than an over-report: the next rule
// to ask the same question would get the old answer and nobody would know the two had diverged.
// Every site is an `async` function with a `try`/`catch`/`finally` in this tree, and each is a real
// mixed-return function whose end genuinely cannot be reached, so the findings are false positives
// rather than corruption.
func consistentReturnCanRunOffEnd(node *ast.Node) bool {
	graph := control_flow_graph.Build(node, control_flow_graph.Hooks[struct{}]{})
	if graph == nil {
		return true
	}
	return graph.EndReachable
}

// consistentReturnName renders upstream's `getFunctionNameWithKind`, plus the Program case.
//
// Not the shelf's `arrayCallbackFunctionNameWithKind`, which answers a narrower question: it knows
// only `arrow function`, `function` and `function 'name'`, because those are the only renderings
// its corpus asserts. This rule's corpus asserts nine more -- `method 'foo'`, `getter 'foo'`,
// `setter 'foo'`, `static method 'foo'`, `private method #foo`, `static private method #foo`,
// `async function 'foo'`, `generator function 'foo'`, `async generator function 'foo'` -- so the
// two are prefixed apart rather than merged. Merging would widen every message the other rule
// renders.
//
// `capitalise` is the `upperCaseFirst` upstream applies to the per-return messages and not to
// `missingReturn`. The corpus asserts both spellings, and the Program case asserts them in
// opposite directions, so the flag is load-bearing rather than cosmetic.
//
// Every rendering below was measured by driving the installed rule rather than derived from the
// helper's source.
func consistentReturnName(node *ast.Node, capitalise bool) string {
	if node.Kind == ast.KindSourceFile {
		if capitalise {
			return "Program"
		}
		return "program"
	}

	var tokens []string

	// The proposal spells `static` before a visibility word, which is why the order here is static
	// then private rather than the reverse.
	if ast.HasSyntacticModifier(node, ast.ModifierFlagsStatic) {
		tokens = append(tokens, "static")
	}
	privateName := ""
	if name := node.Name(); name != nil && name.Kind == ast.KindPrivateIdentifier {
		tokens = append(tokens, "private")
		// `Text()` on a private identifier already carries the leading hash, and upstream renders
		// it unquoted where an ordinary name is quoted.
		privateName = name.Text()
	}
	if ast.HasSyntacticModifier(node, ast.ModifierFlagsAsync) {
		tokens = append(tokens, "async")
	}
	if consistentReturnIsGenerator(node) {
		tokens = append(tokens, "generator")
	}

	switch node.Kind {
	case ast.KindConstructor:
		// Upstream returns the bare word with no modifiers at all, discarding whatever it pushed.
		return consistentReturnCapitalise("constructor", capitalise)
	case ast.KindGetAccessor:
		tokens = append(tokens, "getter")
	case ast.KindSetAccessor:
		tokens = append(tokens, "setter")
	case ast.KindMethodDeclaration:
		tokens = append(tokens, "method")
	default:
		if node.Kind == ast.KindArrowFunction {
			tokens = append(tokens, "arrow")
		}
		tokens = append(tokens, "function")
		// A function expression assigned to a property or a class field is a `method` upstream,
		// because it reads the PARENT's node type rather than the function's. Measured:
		// `var o = { foo: function() {...} }` renders "method 'foo'", not "function 'foo'".
		if node.Kind == ast.KindFunctionExpression && node.Parent != nil {
			switch node.Parent.Kind {
			case ast.KindPropertyAssignment, ast.KindPropertyDeclaration:
				tokens = tokens[:len(tokens)-1]
				tokens = append(tokens, "method")
			}
		}
	}

	if privateName != "" {
		tokens = append(tokens, privateName)
		return consistentReturnCapitalise(strings.Join(tokens, " "), capitalise)
	}
	if name := consistentReturnStaticName(node); name != "" {
		tokens = append(tokens, "'"+name+"'")
	}
	return consistentReturnCapitalise(strings.Join(tokens, " "), capitalise)
}

// consistentReturnStaticName is the name upstream quotes into the message, or "" for none.
//
// Upstream reads `getStaticPropertyName(parent)` for a member and falls back to `node.id.name`, so
// a computed key whose value the syntax does not settle renders no name at all: measured,
// `class A { [x]() { if (a) return true; } }` renders "method." with no name, while
// `class A { ['computed']() {...} }` renders "method 'computed'". That is exactly the question
// `property.Name` answers, including its refusal to read a bare identifier through brackets, so it
// is a caller rather than a reimplementation.
func consistentReturnStaticName(node *ast.Node) string {
	// A function expression takes its parent's key when it has one, which is what makes
	// `{ foo: function() {} }` render `'foo'` despite the function being anonymous.
	if node.Kind == ast.KindFunctionExpression && node.Parent != nil {
		switch node.Parent.Kind {
		case ast.KindPropertyAssignment, ast.KindPropertyDeclaration:
			if name, ok := property.Name(node.Parent.Name(), property.Static); ok {
				return name
			}
		}
	}
	name := node.Name()
	if name == nil {
		return ""
	}
	if text, ok := property.Name(name, property.Static); ok {
		return text
	}
	return ""
}

// consistentReturnCapitalise is upstream's `upperCaseFirst`, applied only to the per-return
// messages.
func consistentReturnCapitalise(text string, capitalise bool) string {
	if !capitalise || text == "" {
		return text
	}
	return strings.ToUpper(text[:1]) + text[1:]
}

// consistentReturnIsGenerator says whether a function carries the `*`.
func consistentReturnIsGenerator(node *ast.Node) bool {
	switch node.Kind {
	case ast.KindFunctionDeclaration:
		return node.AsFunctionDeclaration().AsteriskToken != nil
	case ast.KindFunctionExpression:
		return node.AsFunctionExpression().AsteriskToken != nil
	case ast.KindMethodDeclaration:
		return node.AsMethodDeclaration().AsteriskToken != nil
	}
	return false
}

// consistentReturnReportRange is where the `missingReturn` finding points.
//
// Upstream computes a `loc` per node kind rather than reporting the whole function, and the four
// answers are distinct enough that a port pointing at the function would pass every message-id
// fixture while being wrong on all of them:
//
//	Program                 line 1 column 0, the head of the file
//	ArrowFunctionExpression the `=>` token
//	a method or an object    the key
//	  literal method
//	anything else           the function's name, or the `function` keyword when it has none
//
// The columns the corpus asserts pin all four. `f(() => { if (a) return true; })` reports at column
// 6, which is the `=>`; `f(function() {...})` reports at column 3, which is the `function` keyword;
// `f(function foo() {...})` reports at column 12, which is the name rather than the keyword; and
// `var obj = {foo() {...}}` reports at column 12, which is the key.
func consistentReturnReportRange(ctx rule.Context, node *ast.Node) core.TextRange {
	if node.Kind == ast.KindSourceFile {
		// The head of the program. A zero-length range at the first character rather than the whole
		// file, so the finding points at line 1 column 1 the way upstream's `{line: 1, column: 0}`
		// does.
		//
		// A mutation removing this arm survives, and it is equivalent rather than untested:
		// `TokenRange` on a source file also begins at offset 0, so both paths report at the same
		// position. Its INVERSE, moving the range to offset 1, fails five lines, which proves the
		// fixtures can see the program span. Kept because it states the intent -- upstream reports
		// the head of the program rather than the whole file -- and because it does not depend on
		// what `TokenRange` happens to answer for a construct with no leading token.
		return core.NewTextRange(0, 0)
	}

	if node.Kind == ast.KindArrowFunction {
		// The `=>` token. Found by scanning from the end of the parameter list rather than by
		// taking a child, because the arrow is a token rather than a node here.
		if arrow := node.AsArrowFunction().EqualsGreaterThanToken; arrow != nil {
			return rule.TokenRange(ctx.SourceFile, arrow)
		}
		return rule.TokenRange(ctx.SourceFile, node)
	}

	// A method, an accessor or a constructor points at its key, which upstream reads as
	// `node.parent.key`. There is deliberately no arm for those kinds here, because the fallback at
	// the end of this function already reads `node.Name()` and a method's name IS its key, so an
	// explicit arm would call the same accessor and return the same range.
	//
	// This was measured rather than assumed. An arm was written first, and a mutation neutralising
	// it survived the whole fixture set including five cases asserting a method's span against a
	// modifier. Its INVERSE -- returning the whole node from that arm -- fails nine lines, which
	// proves the fixtures can see the span and that the arm was simply subsumed rather than
	// untested. Removed, with the reasoning here so it is not helpfully restored.
	//
	// A function expression assigned to a property points at the property's key, because upstream
	// reads the parent there too.
	if node.Kind == ast.KindFunctionExpression && node.Parent != nil {
		switch node.Parent.Kind {
		case ast.KindPropertyAssignment, ast.KindPropertyDeclaration:
			if name := node.Parent.Name(); name != nil {
				return rule.TokenRange(ctx.SourceFile, name)
			}
		}
	}

	// The function's own name, or the `function` keyword when it is anonymous. `TokenRange` trims
	// to the first token, which for an anonymous function expression is `function` itself, and for
	// an async one is `async` -- matching upstream's `getFirstToken`, measured at column 20 for
	// `const f = async () => {...}` where the `=>` branch applies instead.
	if name := node.Name(); name != nil {
		return rule.TokenRange(ctx.SourceFile, name)
	}
	return rule.TokenRange(ctx.SourceFile, node)
}
