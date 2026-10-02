package core

import (
	"encoding/json"
	"fmt"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/system-inc/cohere/internal/lint/ecmascript/reference"
	"github.com/system-inc/cohere/internal/lint/rule"
)

var messagePreferConst = rule.Message{
	Id: "preferConst",
	Description: "This binding is declared `let` but nothing ever reassigns it, so the `let` " +
		"claims a mutability the code does not use. A reader has to hold open the possibility " +
		"that the value changes somewhere, and has to scan the rest of the scope to find out that " +
		"it does not. `const` answers that question at the declaration and lets the compiler " +
		"reject a future write that was not intended. Declare it `const`.",
}

// PreferConstDestructuring selects how a destructuring declaration is judged.
type PreferConstDestructuring int

const (
	// PreferConstDestructuringAny reports each binding in a pattern on its own merits, which is
	// upstream's default. `let {a, b} = obj; b = 0;` reports `a`.
	PreferConstDestructuringAny PreferConstDestructuring = iota
	// PreferConstDestructuringAll reports a pattern only when every binding in it could be const,
	// so a pattern that mixes reassigned and never-reassigned bindings is left alone. `const`
	// applies to the whole declarator rather than to one name, so under `any` the finding names
	// something the reader cannot act on without splitting the declaration.
	PreferConstDestructuringAll
)

// UnmarshalJSON decodes the option's two spellings from a configuration file.
//
// The option arrives as the JSON string "any" or "all" and the decoder is a plain json.Unmarshal
// into this struct, so without this the string lands on an int field and the whole option object
// fails to decode. That failure is quiet in the direction that matters: the rule then runs on its
// zero value, which is "any", and a project that asked for "all" silently gets the other answer.
//
// An unrecognized value is an error rather than a fallback to the default, because ESLint's schema
// is an enum with additionalProperties false: a typo is a configuration mistake and reporting it is
// more useful than guessing which of the two the author meant.
func (destructuring *PreferConstDestructuring) UnmarshalJSON(raw []byte) error {
	var spelling string
	if err := json.Unmarshal(raw, &spelling); err != nil {
		return err
	}
	switch spelling {
	case "any":
		*destructuring = PreferConstDestructuringAny
	case "all":
		*destructuring = PreferConstDestructuringAll
	default:
		return fmt.Errorf("destructuring is %q, which is neither \"any\" nor \"all\"", spelling)
	}
	return nil
}

// PreferConstOptions is the decoded option object.
//
// Both fields are read from ESLint's `meta.schema` rather than from the inventory column, per the
// brief. The schema is one object with exactly two properties and `additionalProperties: false`:
//
//	destructuring: { enum: ["any", "all"] }
//	ignoreReadBeforeAssign: { type: "boolean" }
//
// oxc's `PreferConstConfig` carries the same two under camelCase serde renaming, so the two sources
// agree here and there is nothing to reconcile.
type PreferConstOptions struct {
	Destructuring PreferConstDestructuring

	// IgnoreReadBeforeAssign suppresses a finding when the binding is read at a source position
	// before its declaration. Upstream's stated purpose is avoiding a conflict with
	// `no-use-before-define`: turning such a `let` into a `const` moves the temporal dead zone in a
	// way the author may not want, so the option exists to leave those alone.
	IgnoreReadBeforeAssign bool
}

// PreferConst flags a `let` binding that is never reassigned after it is initialized.
//
//	valid:   const x = 0;
//	valid:   let x = 0; x = 1;
//	valid:   let x; x = 0; x += 1;
//	valid:   let x; { x = 0; } foo(x);
//	valid:   for (let x of [1,2,3]) { x = 0; }
//	valid:   let predicate; [typeNode.returnType, ...predicate] = foo();
//	invalid: let x = 1; foo(x);
//	invalid: let x; x = 0;
//	invalid: for (let x of [1,2,3]) { foo(x); }
//	invalid: let { foo, bar } = baz;
//
// # This rule inverts its siblings, and the inversion decides the whole design
//
// `no-const-assign` and `no-class-assign` report a write that should not have happened, so a write
// their detection misses is a missed report on code that was already wrong. This rule reports the
// *absence* of any write, so a write its detection misses turns correct code into a finding, and
// the fix it offers rewrites that correct code into a `const` that throws at runtime. A missed
// write category here is a false positive with a broken repair attached, which is the strictly
// worse direction.
//
// Every judgment below therefore fails toward silence. Where the rule cannot see the whole picture,
// it says nothing rather than guessing.
//
// # Why this reads the checker
//
// Same reason as the siblings: the discrimination is name resolution. Upstream's clean cases
// include `let x = 0; { let x = 1; foo(x); } x = 0;`, where the inner `x` genuinely is const-able
// and the outer one is not, and the two are spelled identically. Node identity rather than
// declaration kind, because both anchors here are `KindVariableDeclaration` and kind cannot
// separate them at all. The distinguishing input is `let a = 1; { let a = 1; a = 2; }`, where a
// kind comparison sees the inner write as a write to the outer binding and silences a finding that
// identity reports; it has its own fixture and its own mutant.
//
// # The structural half does not come off the shelf here, and that is measured
//
// `no-const-assign` uses `ast.IsWriteAccess` as its write detector and documents it as probed. It
// was probed again for this rule, on this rule's own corpus, because the brief is right that a
// shelf function's doc comment is a claim. It has a gap:
//
//	[...w] = []        IsWriteAccess reports false
//	({...w} = {})      IsWriteAccess reports false
//	[a, ...w] = []     IsWriteAccess reports false
//
// A rest element inside a destructuring *assignment target* is a `KindSpreadElement` or
// `KindSpreadAssignment` whose identifier the accessor declines. For the sibling rules that gap is
// a missed report. For this rule it is exactly the false positive described above: upstream carries
// `let predicate; [typeNode.returnType, ...predicate] = foo();` as a *clean* case, and a rule
// trusting the shelf sees zero writes to `predicate` and reports it. So the write detection here is
// `reference.WritesToBinding`, which is `ast.IsWriteAccess` with the spread arms added, and the gap
// has its own fixture in both corpora.
//
// That wrapper was written three times, once per rule, before a census read the whole corpus at
// once and lifted the union onto the shelf. The lifted version also closes a gap this rule's own
// wrapper had: a parenthesis sitting directly around the identifier made the climb bail on its
// first step, so `let w = 1; [...(w)] = xs;` read as never written and was reported as convertible
// to `const`. Applying that suggestion produces code that throws `TypeError: Assignment to constant
// variable` at runtime. Measured with a control on the unparenthesized form, which was always
// correct.
//
// # What counts as never reassigned
//
// Two shapes qualify, and they are upstream's:
//
//	an initializer and no writes at all       `let x = 1; foo(x);`
//	no initializer and exactly one write      `let x; x = 0;`
//
// The second needs more than a count, because `const` has to be initialized where it is declared.
// A single write only converts when it sits in the same scope as the declaration and is not
// conditional or repeated, so `let x; { x = 0; } foo(x);` and `let a; while (a = foo());` and
// `let a; if (true) a = 0; foo(a);` are all clean upstream and all clean here. `unconditionalWrite`
// is that test and it is deliberately a whitelist: it walks up from the write to the declaration's
// own scope and refuses if anything on the way could run zero times or more than once. A construct
// it has not been taught reads as conditional, which is the silent direction.
//
// A for-in or for-of head is its own shape. `for (let x of [1,2,3]) { foo(x); }` binds a fresh `x`
// per iteration with no initializer to speak of, so zero writes is enough and upstream reports it.
//
// # What it does not catch, on purpose
//
// `var` is untouched. Upstream leaves `var x = 0;` clean, and converting a `var` changes hoisting
// and scope rather than only mutability, which is a different rule's judgment.
//
// A binding written from a nested function is never reported, whatever the count. `let a; function
// foo() { a = bar(); }` is clean upstream, and the reason is not stylistic: the write may run any
// number of times including zero, and `const` cannot express it. The write detector sees these, and
// `unconditionalWrite` refuses them at the function boundary.
//
// A pattern whose bindings disagree is where the `destructuring` option lives, and the two answers
// are both defensible. See PreferConstDestructuring.
//
// # The fix, and when there is not one
//
// The repair is `let` to `const` over the declaration list's keyword, and it is a fix rather than a
// suggestion: it preserves meaning wherever the rule fires, since the rule only fires when nothing
// reassigns the binding. But it is *conditional*, and the conditions are upstream's:
//
//	every declarator in the list must be const-able     `let x = 1, y = 2; y = 3;` rewrites `y`
//	                                                    to const and breaks the file
//	every declarator must have an initializer, unless   `const y;` does not parse
//	the list is a for-in/of head
//
// Where either fails the finding is still reported and simply carries no fix, which is what
// upstream does and is the honest answer: the diagnosis is right and the repair is a judgment call
// about splitting the declaration.
var PreferConst = rule.Rule{
	Name: "prefer-const",

	// See the doc above: upstream's shadow cases are textually identical to reporting ones and
	// differ only in what the name resolves to.
	NeedsTypeChecker: true,

	Run: func(ctx rule.Context, options any) rule.Listeners {
		settings := PreferConstOptions{}
		if configured, ok := rule.OptionsAs[PreferConstOptions](options); ok {
			settings = configured
		}

		return rule.Listeners{
			ast.KindVariableDeclarationList: func(node *ast.Node) {
				// The engine hands every rule a nil checker when the program could not be built,
				// and this rule can answer nothing without one. Silence is the correct failure
				// here rather than a guess, for the reason in the doc above.
				if ctx.TypeChecker == nil {
					return
				}

				// `let` only. The flags live on the list rather than on the declarator, which is
				// why this anchors on the list. `const` is already right, `var` is a different
				// judgment, and `using` bindings are already constant.
				if node.Flags&ast.NodeFlagsLet == 0 {
					return
				}

				// There is deliberately no `declare` guard here, and the absence is measured rather
				// than an oversight. An ambient `let` looks like it needs one, but neither
				// reporting shape can be reached from an ambient context: TypeScript forbids an
				// initializer on an ambient declaration, which rules out "has an initializer and no
				// writes", and an ambient context holds no statements, which rules out "no
				// initializer and exactly one unconditional write". A guard was written first and
				// then mutated out, and every ambient shape tried still reported nothing, so it was
				// dead code standing in front of a decision already made below. The clean fixture
				// stays, because it pins the OUTCOME rather than the guard.

				sourceFile := ast.GetSourceFileOfNode(node)
				if sourceFile == nil {
					return
				}

				isLoopHead := node.Parent != nil &&
					(node.Parent.Kind == ast.KindForInStatement ||
						node.Parent.Kind == ast.KindForOfStatement)

				declarations := node.AsVariableDeclarationList().Declarations.Nodes

				// A multi-declarator classic `for` head is all-or-nothing. `for (let i = 0, end =
				// 10; i < end; ++i) {}` has an `end` nothing writes and an `i` the update clause
				// writes every iteration, and upstream reports neither: the keyword is shared, so
				// naming `end` alone offers advice the reader cannot take without splitting a
				// declaration that the loop's own grammar holds together. Measured against
				// upstream's corpus, which carries this exact input and its in-a-function twin as
				// clean, and which the first execution of these fixtures caught.
				if node.Parent != nil && node.Parent.Kind == ast.KindForStatement &&
					len(declarations) > 1 {
					for _, declaration := range declarations {
						anyDeclined := false
						forEachBoundName(declaration.AsVariableDeclaration().Name(),
							func(boundName *ast.Node) {
								if !bindingIsNeverReassigned(ctx, settings, sourceFile, boundName,
									declaration, isLoopHead) {
									anyDeclined = true
								}
							})
						if anyDeclined {
							return
						}
					}
				}

				// Fix eligibility is a property of the whole list rather than of one declarator,
				// because the keyword being rewritten is shared. Computed once, before any
				// reporting, so that a finding on the first declarator knows about the third.
				listIsFixable := true
				for _, declaration := range declarations {
					hasInitializer := declaration.AsVariableDeclaration().Initializer != nil
					// `const y;` does not parse. A for-in/of head is the exception: it has no
					// initializer and does not need one.
					if !hasInitializer && !isLoopHead {
						listIsFixable = false
					}
					forEachBoundName(declaration.AsVariableDeclaration().Name(),
						func(boundName *ast.Node) {
							if !bindingIsNeverReassigned(ctx, settings, sourceFile, boundName,
								declaration, isLoopHead) {
								listIsFixable = false
							}
						})
				}

				// The repair rewrites one keyword that the whole list shares, so it is attached to
				// exactly one of the list's findings however many bindings report. Attaching it to
				// each one produced several identical rewrites of the same three bytes, which the
				// fixture harness correctly refuses to apply rather than guessing which copy wins,
				// and which in the engine would be a pile of overlapping edits for no reason. The
				// first reported binding carries it; the rest carry the diagnosis alone.
				fixAlreadyOffered := false
				for _, declaration := range declarations {
					reportConstCandidates(ctx, settings, sourceFile, node, declaration, isLoopHead,
						listIsFixable, &fixAlreadyOffered)
				}
			},
		}
	},
}

// reportConstCandidates reports the bindings of one declarator that nothing ever reassigns.
//
// Split out from the listener because the `destructuring` option changes the *unit* of the
// judgment, not the judgment itself: under "any" each bound name answers for itself, and under
// "all" the pattern answers as a whole. Both arms call the same predicate.
func reportConstCandidates(ctx rule.Context, settings PreferConstOptions,
	sourceFile *ast.SourceFile, list *ast.Node, declaration *ast.Node, isLoopHead bool,
	listIsFixable bool, fixAlreadyOffered *bool) {
	name := declaration.AsVariableDeclaration().Name()
	if name == nil {
		return
	}

	var boundNames []*ast.Node
	forEachBoundName(name, func(boundName *ast.Node) {
		boundNames = append(boundNames, boundName)
	})
	if len(boundNames) == 0 {
		return
	}

	isDestructuring := name.Kind == ast.KindObjectBindingPattern ||
		name.Kind == ast.KindArrayBindingPattern

	constable := make([]bool, len(boundNames))
	allConstable := true
	for index, boundName := range boundNames {
		constable[index] = bindingIsNeverReassigned(ctx, settings, sourceFile, boundName,
			declaration, isLoopHead)
		if !constable[index] {
			allConstable = false
		}
	}

	// Under "all", a pattern that mixes the two answers reports nothing. `const` applies to the
	// declarator, so naming one binding inside a pattern the reader cannot convert without
	// restructuring is advice they cannot take.
	if settings.Destructuring == PreferConstDestructuringAll && isDestructuring && !allConstable {
		return
	}

	for index, boundName := range boundNames {
		if !constable[index] {
			continue
		}
		if listIsFixable && !*fixAlreadyOffered {
			*fixAlreadyOffered = true
			ctx.ReportNodeWithFixes(boundName, messagePreferConst, letKeywordToConst(ctx, list))
			continue
		}
		ctx.ReportNode(boundName, messagePreferConst)
	}
}

// bindingIsNeverReassigned decides whether one bound name could have been declared `const`.
//
// The whole rule's judgment is here, and every branch that returns false is a place the rule
// declines to speak. That asymmetry is deliberate: see the inversion note on PreferConst.
func bindingIsNeverReassigned(ctx rule.Context, settings PreferConstOptions,
	sourceFile *ast.SourceFile, boundName *ast.Node, declaration *ast.Node, isLoopHead bool) bool {
	if boundName == nil || boundName.Kind != ast.KindIdentifier {
		return false
	}

	// Resolved through the checker rather than taken as the declarator node directly, so that both
	// sides of the later comparison answer the same question. Asking the AST for one side and the
	// checker for the other compares two things that happen to agree today.
	anchor := declarationAnchoredAt(ctx, boundName)
	if anchor == nil {
		return false
	}

	writes := writesResolvingTo(ctx, sourceFile, boundName, anchor)

	if settings.IgnoreReadBeforeAssign {
		// Upstream splits this option into two tests by whether the declarator has an initializer,
		// and the split is load-bearing rather than tidy. With an initializer the write is the
		// declaration itself, so "read before assign" can only mean a read at a source position
		// before the declaration. With no initializer the assignment is a separate statement, so
		// the question is whether any read sits before THAT write, and a read after the declaration
		// still counts: `let x; function foo() { bar(x); } x = 0;` is upstream's own case, clean
		// under the option and reported without it, and a single position test against the
		// declaration answers it wrong whichever position it picks.
		boundary := declaration.Pos()
		if declaration.AsVariableDeclaration().Initializer == nil && len(writes) == 1 {
			boundary = writes[0].Pos()
		}
		if readsBeforePosition(ctx, sourceFile, boundName, anchor, boundary) {
			return false
		}
	}

	// A for-in/of head rebinds on every iteration and needs no initializer, so no writes at all is
	// the whole test. `for (let x of [1,2,3]) { x = 0; }` has one and is correctly declined.
	if isLoopHead {
		return len(writes) == 0
	}

	if declaration.AsVariableDeclaration().Initializer != nil {
		return len(writes) == 0
	}

	// No initializer. A `const` must be initialized at its declaration, so exactly one write can
	// stand in for the initializer and only where that write is guaranteed to run exactly once in
	// the declaration's own scope.
	if len(writes) != 1 {
		return false
	}

	// A read-write such as `x += 1` or `x++` cannot be the initializing write: it reads the binding
	// before the declaration ever gave it a value. `let x; x += 1;` is clean upstream.
	if !isWriteOnly(writes[0]) {
		return false
	}

	return unconditionalWrite(writes[0], declaration)
}

// writesResolvingTo collects every occurrence in the file that assigns to one declaration.
//
// The whole file rather than any bounded subtree, for the sibling rules' reason: a write can sit
// before the declaration, after it, or several scopes down inside a callback, and all three are the
// same binding. That matters more here than there, because a write this walk fails to find is a
// false positive rather than a missed report.
func writesResolvingTo(ctx rule.Context, sourceFile *ast.SourceFile, boundName *ast.Node,
	anchor *ast.Node) []*ast.Node {
	var found []*ast.Node
	var visit func(*ast.Node)
	visit = func(current *ast.Node) {
		if current == nil {
			return
		}
		// The declarator's own name is not a write: its parent is the declarator, which
		// `reference.WritesToBinding` declines. No explicit exclusion is needed and adding one would be
		// untested code.
		//
		// The text comparison is a pre-filter rather than a discrimination, since symbol identity
		// already implies it. It is here because it is far cheaper than a checker call and this
		// walk visits every identifier in the file.
		if current.Kind == ast.KindIdentifier &&
			current.Text() == boundName.Text() &&
			reference.WritesToBinding(current) &&
			resolvesToDeclaration(ctx, current, anchor) {
			found = append(found, current)
		}
		current.ForEachChild(func(child *ast.Node) bool {
			visit(child)
			return false
		})
	}
	visit(sourceFile.AsNode())
	return found
}

// readsBeforePosition reports whether the binding is read at a source position before `position`.
//
// Only consulted under `ignoreReadBeforeAssign`. Upstream frames this as avoiding a conflict with
// `no-use-before-define`, and the position comparison is upstream's too: it compares source offsets
// rather than reasoning about execution order, so `function foo() { bar(x); } let x = 0;` counts as
// a read before the declaration even though the call happens later. That is a coarse test and it is
// reproduced rather than improved on, because the option exists to suppress a class of findings and
// a narrower test would suppress fewer of them than the option's users expect.
func readsBeforePosition(ctx rule.Context, sourceFile *ast.SourceFile, boundName *ast.Node,
	anchor *ast.Node, position int) bool {
	found := false
	var visit func(*ast.Node)
	visit = func(current *ast.Node) {
		if current == nil || found {
			return
		}
		if current.Kind == ast.KindIdentifier &&
			current.Text() == boundName.Text() &&
			current.Pos() < position &&
			!reference.WritesToBinding(current) &&
			resolvesToDeclaration(ctx, current, anchor) {
			found = true
			return
		}
		current.ForEachChild(func(child *ast.Node) bool {
			visit(child)
			return false
		})
	}
	visit(sourceFile.AsNode())
	return found
}

// isWriteOnly reports whether an occurrence writes without first reading.
//
// `x = 0` qualifies; `x += 1` and `x++` do not, because they read the binding to compute the value
// they store. That distinction is what keeps `let x; x = 0; x += 1;` and `let a; for (const x of
// [1,2,3]) { foo(++a); }` clean, and it is upstream's `is_write() && !is_read()`.
//
// A destructuring target is write-only: `({a} = obj)` stores into `a` without reading it. The
// default in `({a = 0} = obj)` is a read of the *source*, not of `a`, so it stays write-only, and
// upstream reports that case.
func isWriteOnly(identifier *ast.Node) bool {
	for parent := identifier.Parent; parent != nil; parent = parent.Parent {
		switch parent.Kind {
		case ast.KindBinaryExpression:
			binary := parent.AsBinaryExpression()
			return binary.OperatorToken != nil &&
				binary.OperatorToken.Kind == ast.KindEqualsToken

		case ast.KindPrefixUnaryExpression, ast.KindPostfixUnaryExpression:
			// `++x` and `x--` read before they write.
			return false

		case ast.KindForInStatement, ast.KindForOfStatement:
			return true

		case ast.KindArrayLiteralExpression,
			ast.KindObjectLiteralExpression,
			ast.KindParenthesizedExpression,
			ast.KindSpreadElement,
			ast.KindSpreadAssignment,
			ast.KindShorthandPropertyAssignment,
			ast.KindPropertyAssignment:
			// Destructuring wrappers. The question is decided at the assignment above them.

		default:
			return false
		}
	}
	return false
}

// unconditionalWrite reports whether a single write is guaranteed to run exactly once in the
// declaration's own scope.
//
// This is what stands in for oxc's ancestor walk over control-flow node kinds, and it is a
// whitelist rather than a blacklist on purpose. oxc enumerates the constructs that make a write
// conditional and treats anything unlisted as fine; that direction ships a false positive the first
// time a construct is left out. This enumerates the constructs a write may pass through and treats
// anything unlisted as conditional, so an unfamiliar shape costs a missed report instead.
//
// The climb stops at the declaration's own statement list. A write in a nested block, a loop body,
// an `if` branch, a `switch` case, a `try`, or any function is refused, which is what makes
// `let x; { x = 0; } foo(x);` and `let a; while (a = foo());` and `let a; function foo() { a =
// bar(); }` clean.
func unconditionalWrite(write *ast.Node, declaration *ast.Node) bool {
	declarationScope := enclosingStatementList(declaration)
	if declarationScope == nil {
		return false
	}

	// A write that arrives through a destructuring assignment target is refused outright. The
	// reason is upstream's and it is about what the repair would have to produce: a `const` must be
	// initialized where it is declared, and there is no way to spell "declare this const and give
	// it the second element of that array" without moving the pattern up into the declaration,
	// which is a restructuring rather than a keyword swap. Upstream reaches the same answer through
	// three separate ancestor arms (a member expression anywhere in the target, an enclosing block,
	// and a bare array expression) that between them refuse every destructuring shape its corpus
	// carries; this refuses the category once, which is the quieter direction the inversion note
	// asks for and which the whole `predicate` family of clean cases depends on.
	if writeArrivesThroughDestructuring(write) {
		return false
	}

	for current := write; current != nil; current = current.Parent {
		switch current.Kind {
		case ast.KindIdentifier,
			ast.KindBinaryExpression,
			ast.KindParenthesizedExpression,
			ast.KindArrayLiteralExpression,
			ast.KindObjectLiteralExpression,
			ast.KindSpreadElement,
			ast.KindSpreadAssignment,
			ast.KindShorthandPropertyAssignment,
			ast.KindPropertyAssignment,
			ast.KindExpressionStatement:
			// A plain assignment statement and the destructuring wrappers it may contain. Anything
			// here runs exactly when its enclosing statement list runs.

		default:
			// Reached the construct that holds the statement. It is the write's home only if it is
			// the same one the declaration lives in.
			return current == declarationScope
		}
	}
	return false
}

// enclosingStatementList returns the construct whose statement list directly holds a node.
//
// Deliberately the *directly* enclosing one rather than the enclosing function: a write inside a
// nested block of the same function is still conditional in the sense that matters, because the
// block may be a loop body or an `if` consequent. Upstream reaches the same answers through a scope
// comparison plus a control-flow list; this reaches them with one test, and where the two disagree
// this one is quieter.
func enclosingStatementList(node *ast.Node) *ast.Node {
	for current := node.Parent; current != nil; current = current.Parent {
		switch current.Kind {
		case ast.KindSourceFile,
			ast.KindBlock,
			ast.KindModuleBlock,
			ast.KindCaseClause,
			ast.KindDefaultClause,
			ast.KindClassStaticBlockDeclaration:
			return current
		}
	}
	return nil
}

// letKeywordToConst builds the repair that rewrites the list's `let` keyword.
//
// The span is the keyword alone rather than the whole declaration. oxc replaces the entire
// declaration span deliberately, so that an overlapping fix from another rule conflicts and only
// one lands; our engine resolves overlaps itself, so the narrow span is both correct and easier to
// read in a diff.
//
// The keyword is located by scanning forward from the list's start for the three characters, rather
// than assuming offset zero. A list's `Pos()` sits before leading trivia, so `/* c */ let x = 1`
// would otherwise rewrite the comment.
func letKeywordToConst(ctx rule.Context, list *ast.Node) rule.Fix {
	keyword := rule.TokenRange(ctx.SourceFile, list)
	return rule.ReplaceRange(core.NewTextRange(keyword.Pos(), keyword.Pos()+len("let")), "const")
}

// writeArrivesThroughDestructuring reports whether an identifier is written by being a target
// inside an array or object destructuring assignment, rather than by a plain `x = value`.
//
// The climb is the same shape as the shelf write detector's and for the same reason: a target
// nests, so `[, {foo: typeNode.returnType, ...predicate}] = foo()` reaches its assignment through
// object, array and spread nodes in turn. Seeing any array or object literal on the way up to the
// assignment operator is the whole test, because in an assignment target position those literals
// are patterns rather than constructed values.
func writeArrivesThroughDestructuring(write *ast.Node) bool {
	sawPattern := false
	child := write
	for parent := write.Parent; parent != nil; parent = parent.Parent {
		switch parent.Kind {
		case ast.KindArrayLiteralExpression, ast.KindObjectLiteralExpression:
			sawPattern = true

		case ast.KindParenthesizedExpression,
			ast.KindSpreadElement,
			ast.KindSpreadAssignment,
			ast.KindShorthandPropertyAssignment,
			ast.KindPropertyAssignment:
			// Wrappers a target sits inside. The question is decided further up.

		case ast.KindBinaryExpression:
			binary := parent.AsBinaryExpression()
			if binary.OperatorToken == nil ||
				!ast.IsAssignmentOperator(binary.OperatorToken.Kind) ||
				ast.SkipParentheses(binary.Left) != child {
				return false
			}
			return sawPattern

		case ast.KindForInStatement, ast.KindForOfStatement:
			return sawPattern && parent.AsForInOrOfStatement().Initializer == child

		default:
			return false
		}
		child = parent
	}
	return false
}
