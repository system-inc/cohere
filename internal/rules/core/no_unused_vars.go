package core

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/rule"
	"github.com/system-inc/verify/internal/utilities/ecmascript/reference"
)

var messageNoUnusedVars = rule.Message{
	Id: "noUnusedVars",
	Description: "This binding is declared and nothing ever reads it. A name that is written but " +
		"never read is almost always the residue of an edit that moved on: an import whose call " +
		"site was deleted, a parameter left behind when a signature changed, a variable holding a " +
		"value nobody asked for. It costs nothing at runtime, which is what lets it accumulate, " +
		"and it costs the next reader real time, because an unused name reads exactly like a used " +
		"one until you search the file and find nothing. Delete it, or prefix it with an " +
		"underscore to say the omission is deliberate.",
}

// NoUnusedVars flags a variable, import, parameter, or type that nothing reads.
//
//	valid:   const x = 10; alert(x);
//	valid:   function getY([, y]) { return y; }
//	valid:   export const x = 1;
//	valid:   const _unused = compute();
//	valid:   import type { T } from './t'; const a: T = read();
//	invalid: const x = 10;
//	invalid: function add(a, b) { return a; }
//	invalid: import { unusedThing } from './module';
//	invalid: let z = 0; z = z + 1;
//
// # What this rule decides, and at what scope
//
// The question here is deliberately narrow: is this binding read anywhere within the file that
// declares it. That is not the same question as "is this symbol used anywhere in the program", and
// the difference is the whole reason this rule can be a per-file gate at all.
//
// A per-file rule cannot see who imports the file it is handed, so every exported binding is
// treated as used. That is upstream's answer too, for the same reason, and it is the conservative
// direction: the alternative is reporting a component's only export as dead because this file
// happens not to call it. The program-wide claim is a real and different analysis and it already
// exists here as `verify --unused`, which is opt-in precisely so that reading every file in the
// program does not poison the per-file findings cache. See `rule.ReadsProgram` for why that
// separation is structural rather than stylistic: a rule reading beyond its own file, cached on
// that file's hash, serves a stale answer forever and nothing notices.
//
// So this rule declines the whole-program question rather than approximating it. Two analyses, two
// scopes, neither pretending to be the other.
//
// # Why this does not use the liveness dataflow, and what that costs
//
// `no_useless_assignment_analysis.go` in this package is a real backward-liveness dataflow over the
// control-flow graph, and the temptation to route this rule through it is strong: oxc spends
// roughly 990 lines in `usage.rs` hand-matching patterns — a self-reference inside a function's own
// body, a variable only ever reassigned to itself, a write nothing reads — that a dataflow answers
// in one query. oxc has an SSA crate in the same repository and this rule does not touch it, which
// reads like an omission.
//
// It is not an omission this port should correct, and the reason is the harness rather than the
// analysis. The differential compares our findings against oxlint's. A rule that is *more* correct
// than upstream produces a difference, and a difference costs the harness its meaning whether the
// extra finding is right or wrong. Measured on upstream's own corpus, the two approaches genuinely
// disagree: `test_vars_self_use` ships `chain = chain.extend()` inside a `for` loop as a PASS and
// the same statement outside a loop as a FAIL. A liveness pass sees a store whose value no later
// read observes in both cases and would report both. Upstream's answer is the loop-shape test at
// `usage.rs:612` (`is_in_for_loop_test_or_update`) and `:634` (`is_in_loop_body`), which is a
// syntactic proxy for the flow fact that a loop's back edge makes the store observable on the next
// iteration.
//
// The dataflow would get that particular case right and would still be a divergence, because it
// would also change the answer on shapes upstream never wrote a case for and where nobody has
// established which answer is correct. So the pattern catalogue is reproduced, upstream's loop
// exemptions included, and the flow analysis is left where it already ships a measured win.
//
// The cost is stated rather than hidden: this rule is exactly as blind as upstream is, on exactly
// the shapes upstream is blind on. That is the subset this port can show correct.
//
// # What this reports against upstream, measured
//
// 408 of 408 clean cases stay clean, 301 of 303 reporting cases report, and all 51 JSX cases agree.
// The two that do not are named with their causes in `noUnusedVarsKnownGaps`, and one of them is a
// case upstream cannot report either and documents as such in its own test file.
//
// Zero false positives is the property that matters most here and it is the one held fixed: every
// change that closed a gap was measured against all 408 clean cases first, and two changes that
// closed gaps at the cost of a clean case were reworked rather than kept. A rule that misses
// something costs a finding nobody had; a rule that accuses wrongly costs trust in every finding it
// makes.
//
// # The leading-underscore default, which is a real divergence between the two upstreams
//
// oxc ignores any binding whose name begins with `_` under default options; ESLint does not. This
// is not a reading of the sources, it is measured on both: ESLint's own Linter API reports `_a` in
// `const _a = 1;` with no options configured, and oxc's `ignored.rs:455` resolves
// `IgnorePattern::Default` to `haystack.starts_with('_')`. The corpus corroborates it independently
// at `tests/oxc.rs:89`, where `let _a = 1` is a PASS carrying only an `argsIgnorePattern` — a case
// that can only be clean if the vars default already ignores the underscore.
//
// oxc wins, because oxc is what the differential runs against. ESLint's answer is recorded here so
// the next reader does not helpfully correct this back.
//
// One sub-case is not symmetric and is easy to miss: a parameter named exactly `_` is NOT ignored
// under the default args pattern (`ignored.rs:424`), while a variable named `_` is. That asymmetry
// is upstream's and is reproduced.
//
// # This underscore default is the ENTIRE measured gap against `@typescript-eslint/no-unused-vars`
//
// Measured on the ahra tree on 2026-09-05, driving the installed 8.67.0 rule through the ESLint
// Linter API over 4,961 files and comparing against this rule's own dry run over the same tree.
// The namespaced rule reports three findings; this rule reports two of them, at identical
// file, line and column:
//
//	modules/google/analytics/AnalyticsTypes.ts:30:6   AnalyticsColumnWidthsInterface   both report
//	modules/planetscale/PlanetScaleTypes.ts:6:6       ParsedRowInterface               both report
//	modules/pensieve/PensieveBootstrap.test.ts:509:58 _nextContent                     only upstream
//
// The single missing finding is a destructured binding named `_nextContent`, and it is missing
// because of the underscore default above rather than because of anything structural. Setting
// `varsIgnorePattern` to `^$` in the config reproduces all three at exactly upstream's positions,
// with no code change and no other movement anywhere in the tree. That measurement is why the gap
// is recorded here rather than closed: which default this tree wants is a configuration decision,
// and reversing it in the rule would reverse it for the oxc differential too.
//
// Worth stating plainly because it has been assumed twice in the other direction: the missing
// finding is NOT evidence that this rule is blind to type declarations. It is not. A seeded probe
// carrying an unused `type` alias, an unused `interface` and an unused `const` reports all three,
// and the two type findings above are ones this rule already makes on the real tree. The
// `bindingType` arm in `collectCandidateBindings` is what does it. A port of the namespaced rule
// undertaken to recover type-declaration coverage would be recovering coverage that is already
// here.
var NoUnusedVars = rule.Rule{
	Name: "no-unused-vars",

	// Symbol identity, not name matching. Two bindings can share a name across scopes and a read of
	// one must not keep the other alive: `let x; { let x; log(x); }` reads the inner binding only,
	// and a name-matching rule calls both used. That is the quiet direction of the mistake, and for
	// this rule the quiet direction is where every missed finding lives.
	NeedsTypeChecker: true,

	Run: func(ctx rule.Context, options any) rule.Listeners {
		settings := resolveNoUnusedVarsOptions(options)

		return rule.Listeners{
			// One pre-pass over the file rather than a listener per declaration kind. The rule has
			// to know every read in the file before it can judge any declaration, and reads can sit
			// above a declaration inside a hoisted function as easily as below it. `KindSourceFile`
			// fires before its children, so the gather and the report both happen here.
			ast.KindSourceFile: func(node *ast.Node) {
				if ctx.TypeChecker == nil {
					return
				}

				sourceFile := node.AsSourceFile()
				if sourceFile == nil {
					return
				}

				// Upstream skips these entirely, and the reasons are not stylistic. A `.d.ts`
				// declares rather than defines and its bindings are consumed by other files by
				// construction, so every one of them would report. Vue, Svelte and Astro put reads
				// in a template the linter never parses, so a binding used in the markup looks
				// unused in the script.
				//
				// The `.d.ts` arm is SUBSUMED rather than load-bearing, and it is kept anyway.
				// Measured: with this suffix test disabled, `interface Unreferenced {}` in a
				// `a.d.ts` still reports nothing, because typescript-go marks every declaration in
				// a declaration file ambient and `isInsideSignatureOrAmbientDeclaration` already
				// declines it. A mutant disabling this line therefore survives every fixture, and
				// the fixture asserting it passes for a reason one layer down from what it names.
				//
				// It stays because the two guards answer different questions and only coincide
				// today: this one is about the FILE and would keep holding if a future `.d.ts`
				// carried a non-ambient binding, which is the shape the ambient test would miss.
				// Recorded here so the next reader does not delete it believing it does nothing,
				// and does not add a fixture believing it can be measured.
				fileName := sourceFile.FileName()
				if strings.HasSuffix(fileName, ".d.ts") ||
					strings.HasSuffix(fileName, ".vue") ||
					strings.HasSuffix(fileName, ".svelte") ||
					strings.HasSuffix(fileName, ".astro") {
					return
				}

				analyzeUnusedBindings(ctx, node, settings)
			},
		}
	},
}

// unusedBindingKind separates the declaration forms, because the exemptions differ per form and so
// does the wording of the finding.
//
// This mirrors upstream's match on `symbol.declaration().kind()` at `mod.rs:281`. Collapsing them
// would be wrong rather than merely coarse: a parameter is exempt under `args: "after-used"` when a
// later parameter is used, a variable never is, and a caught error has its own ignore pattern.
type unusedBindingKind uint8

const (
	// bindingVariable is a `var`, `let`, `const`, or destructured element.
	bindingVariable unusedBindingKind = iota
	// bindingParameter is a function or method parameter.
	bindingParameter
	// bindingImport is an imported name, default or named or namespace.
	bindingImport
	// bindingCaughtError is a `catch (error)` binding.
	bindingCaughtError
	// bindingType is an interface, type alias, or type parameter.
	bindingType
)

// candidateBinding is one declared name this rule may judge, with everything needed to judge it.
//
// The declaring node is carried alongside the name because the exemptions ask about the shape
// around the declaration — whether a parameter is followed by a used one, whether a variable sits
// in a `for` head — and the name alone cannot answer those.
type candidateBinding struct {
	name        *ast.Node
	declaration *ast.Node
	kind        unusedBindingKind
}

// analyzeUnusedBindings gathers every candidate binding in one file, then reports the ones nothing
// reads.
//
// Two passes rather than one, and the order is forced. A read can precede its declaration in source
// order — a hoisted function body referring to a `const` declared below it is legal and common — so
// no single walk can decide a binding at the moment it sees it.
func analyzeUnusedBindings(ctx rule.Context, sourceFile *ast.Node, settings NoUnusedVarsOptions) {
	candidates := collectCandidateBindings(sourceFile)
	if len(candidates) == 0 {
		return
	}

	// The name set is a pre-filter, not a discrimination. Symbol identity still decides every
	// match; this only keeps the walk from paying for a checker call on identifiers that could not
	// possibly resolve to a candidate, and this walk visits every identifier in the file.
	interesting := map[string]bool{}
	for _, candidate := range candidates {
		interesting[candidate.name.Text()] = true
	}

	reads := collectReadSymbols(ctx, sourceFile, interesting)

	for _, candidate := range candidates {
		if isExemptFromUnusedReport(ctx, candidate, candidates, settings, reads) {
			continue
		}
		ctx.ReportNode(candidate.name, messageNoUnusedVars)
	}
}

// collectReadSymbols resolves every identifier that could be a read of a candidate, and returns the
// symbols those reads name.
//
// "Read" here is the rule's own notion and it is narrower than "appears". An identifier in a
// declaring position is not a read of itself, and a pure write is not a read: `y = 5` does not make
// `y` used, which upstream states outright in its documentation ("Write-only variables are not
// considered as used"). Both exclusions are what make the rule find anything at all.
func collectReadSymbols(ctx rule.Context, sourceFile *ast.Node, interesting map[string]bool) map[*ast.Symbol]bool {
	reads := map[*ast.Symbol]bool{}

	var visit func(*ast.Node)
	visit = func(current *ast.Node) {
		if current == nil {
			return
		}

		if current.Kind == ast.KindIdentifier && interesting[current.Text()] {
			for _, symbol := range resolveIdentifierSymbols(ctx, current) {
				if !countsAsRead(current, declaringScopeOf(symbol)) {
					continue
				}
				if !isSelfReferenceWithinOwnDeclaration(ctx, current, symbol) {
					reads[symbol] = true
				}
			}
		}

		current.ForEachChild(func(child *ast.Node) bool {
			visit(child)
			return false
		})
	}
	visit(sourceFile)

	return reads
}

// declaringScopeOf returns the variable scope a symbol is declared in, or nil when that cannot be
// established.
//
// The FIRST declaration is deliberate rather than arbitrary here, and the question it answers is
// "where does this name live", which every declaration of a merged symbol answers identically: a
// symbol's declarations all bind the same name in the same scope, which is what makes them merge.
// A rule asking a different question of a merged symbol would need a different choice.
func declaringScopeOf(symbol *ast.Symbol) *ast.Node {
	if symbol == nil || len(symbol.Declarations) == 0 {
		return nil
	}
	return enclosingVariableScope(symbol.Declarations[0])
}

// countsAsRead reports whether an identifier occurrence loads the binding's value.
//
// The two exclusions are the rule's core judgment and they run in opposite directions, which is why
// they are separated here rather than folded into one predicate.
//
// A declaring name is not a use of itself. Without this every binding would be trivially used by
// its own declaration and the rule would report nothing — the failure mode that looks exactly like
// a clean tree.
//
// A pure write is not a use. `let y = 10; y = 5;` leaves `y` unused, because nothing ever observes
// either value. An update (`y++`, `y += 1`) is deliberately NOT excluded: it loads before it
// stores, so it is a genuine read of the previous value. That asymmetry is upstream's and it is the
// difference between `y = 5` and `y++` in the documentation's own examples.
func countsAsRead(identifier *ast.Node, declaringScope *ast.Node) bool {
	if isDeclaringName(identifier) {
		return false
	}

	// A pure assignment target reads nothing. `reference.WritesToBinding` already handles the
	// parenthesized target, the destructuring target, and the shorthand-property positions, and
	// declines a property name on the left of a member access, which is not a binding at all.
	if reference.WritesToBinding(identifier) && !isUpdateWrite(identifier) {
		return false
	}

	// A read whose value is thrown away by a comma sequence is not a use. `return (a, 0)` and
	// `(a, 0) + 1` both evaluate `a` and then discard it: the sequence yields its right operand, so
	// nothing downstream can observe the left one. Upstream calls this a discarded read
	// (`usage.rs:709`) and reports the binding.
	//
	// Only the LEFT operand qualifies, and that is the whole discrimination: `(0, a) + 1` is a
	// genuine read because `a` is the value the sequence produces. Both spellings sit in the corpus
	// and a fix that ignored the side would trade two findings for a false positive.
	if isDiscardedSequenceOperandRead(identifier) {
		return false
	}

	// A read from inside a function that is itself being assigned to the same binding is not a
	// use. `function foo(cb) { cb = function(a) { cb(1 + a); }; ... }` stores a closure into `cb`
	// whose only reader is `cb` itself, so nothing outside the cycle observes the binding and
	// upstream reports the parameter.
	//
	// This is deliberately narrow, and the narrowness is upstream's rather than caution of ours.
	// Two neighbouring shapes are upstream PASS cases and must stay clean: an immediately-invoked
	// function expression (`cb = function(a){ return cb(1+a); }()`, which runs now and so really
	// does read `cb`) and a sequence whose value is the binding itself (`cb = (function(a){...},
	// cb)`). Upstream ships both spellings of the sequence form in its fail list too, commented
	// out, at `tests/eslint.rs:743` and `:745` — it knows they should report and cannot make them.
	// Those two stay silent here for the same reason, which is a reproduced gap and not an
	// oversight.
	if isReadInsideAClosureAssignedToItself(identifier) {
		return false
	}

	// An update or a compound assignment whose only purpose is to feed the binding back into
	// itself is not a use. `var a = 0; a = a + 1;` and `a++` both load the binding, but nothing
	// ever observes the result, so upstream calls them unused and documents it outright: "A read
	// for a modification of itself is not considered as used."
	//
	// This is `usage.rs:445`'s `is_self_reassignment`, and it is the single largest source of
	// findings this rule would otherwise miss: it accounted for 129 of the corpus's failing cases
	// going silent before it existed.
	if isSelfReassignmentRead(identifier, declaringScope) {
		return false
	}

	return true
}

// isUpdateWrite reports whether a write to this identifier also loads it first.
//
// `x++`, `x--`, and every compound assignment (`x += 1`, `x ||= y`) read the current value before
// storing the new one, so they keep an otherwise-unused binding alive in a way a plain `x = 1` does
// not.
func isUpdateWrite(identifier *ast.Node) bool {
	for current := identifier; current != nil && current.Parent != nil; current = current.Parent {
		parent := current.Parent
		switch parent.Kind {
		case ast.KindParenthesizedExpression:
			continue

		case ast.KindPrefixUnaryExpression:
			return reference.IsUpdateOperator(parent.AsPrefixUnaryExpression().Operator)

		case ast.KindPostfixUnaryExpression:
			return reference.IsUpdateOperator(parent.AsPostfixUnaryExpression().Operator)

		case ast.KindBinaryExpression:
			binary := parent.AsBinaryExpression()
			if binary.Left != current {
				return false
			}
			// A plain `=` stores without loading; every other assignment operator loads first.
			return binary.OperatorToken != nil &&
				binary.OperatorToken.Kind != ast.KindEqualsToken &&
				ast.IsAssignmentOperator(binary.OperatorToken.Kind)

		default:
			return false
		}
	}
	return false
}

// isDeclaringName reports whether an identifier is the name being declared rather than a use of it.
//
// Anchored on the parent's own name slot rather than on a kind list, because the same node kind
// appears in both positions: the `x` in `import { x }` declares, the `x` in `f(x)` uses, and both
// are plain identifiers whose parent chain differs only in which field points at them.
func isDeclaringName(identifier *ast.Node) bool {
	parent := identifier.Parent
	if parent == nil {
		return false
	}

	switch parent.Kind {
	case ast.KindVariableDeclaration,
		ast.KindParameter,
		ast.KindBindingElement,
		ast.KindFunctionDeclaration,
		ast.KindFunctionExpression,
		ast.KindClassDeclaration,
		ast.KindClassExpression,
		ast.KindInterfaceDeclaration,
		ast.KindTypeAliasDeclaration,
		ast.KindEnumDeclaration,
		ast.KindEnumMember,
		ast.KindModuleDeclaration,
		ast.KindTypeParameter,
		ast.KindImportClause,
		ast.KindImportEqualsDeclaration,
		ast.KindNamespaceImport,
		ast.KindPropertyDeclaration,
		ast.KindPropertySignature,
		ast.KindMethodDeclaration,
		ast.KindMethodSignature,
		ast.KindGetAccessor,
		ast.KindSetAccessor:
		return parent.Name() == identifier

	// An import specifier declares BOTH of its names, and the export specifier below declares
	// neither of its own, which is why the two cannot share the simple `parent.Name()` test above.
	//
	// `import { a as b } from './m'` puts `a` in the `propertyName` slot and `b` in `name`. Neither
	// is a reference to anything in THIS file: `b` is the binding being created, and `a` names an
	// export of the other module. The plain `parent.Name() == identifier` test answered true only
	// for `b`, so the source name `a` fell through and was walked as an ordinary reference.
	//
	// That is invisible until a module cycle makes the checker resolve it back into this file, and
	// then it is a false positive with no obvious cause. Measured on the ahra tree:
	//
	//	useRouter.ts:11   import { useRouter as useNextRouter } from '.../router/Navigation'
	//	Navigation.ts:31  export { useRouter } from '.../router/hooks/useRouter'
	//	useRouter.ts:16   export function useRouter() { ... }
	//
	// The re-export sends the symbol straight back, so `useRouter` at line 11 resolved to the
	// function declared at line 16 and `no-use-before-define` reported a temporal-dead-zone error
	// on an import binding, which is hoisted and cannot have one. ESLint reports zero on that file
	// because eslint-scope never creates a reference for either half of an import specifier.
	//
	// No imported fixture can see this: it needs a real module graph with a cycle, and upstream's
	// corpus is single-file.
	case ast.KindImportSpecifier:
		return true

	// An export specifier's local name is a genuine reference to the binding it exports, which is
	// why `const y = 1; export { y };` is clean. Its `propertyName` slot, when present, is the
	// local side and the `name` slot is the exported alias, so `export { y as z }` reads `y`.
	case ast.KindExportSpecifier:
		// An export specifier inside `export { x } from './m'` names something in the OTHER
		// module, not the local binding. So `import { resolve } from "path"; export { resolve }
		// from "path";` leaves the import genuinely unused, and upstream reports it, while the
		// same file without the `from` clause is clean because there the specifier does read the
		// local binding.
		//
		// The two spellings are one node kind differing only in whether the enclosing export
		// declaration carries a module specifier, which is why this is asked of the grandparent.
		if exportDeclarationHasModuleSpecifier(parent) {
			return true
		}
		specifier := parent.AsExportSpecifier()
		if specifier.PropertyName != nil {
			return specifier.PropertyName != identifier
		}
		return false

	// A property in an object literal names a key, not a binding. `{ foo: bar }` declares nothing
	// and reads `bar`; the `foo` is a name in the object's own namespace.
	case ast.KindPropertyAssignment:
		return parent.AsPropertyAssignment().Name() == identifier

	case ast.KindPropertyAccessExpression:
		// The right side of a member access is a property name, not a binding reference. Reading
		// `a.b` is a read of `a` and says nothing about any binding named `b`.
		return parent.AsPropertyAccessExpression().Name() == identifier

	case ast.KindQualifiedName:
		// The type-position equivalent of a member access: `N.T` reads `N`, not `T`.
		return parent.AsQualifiedName().Right == identifier

	// A `typeof x` inside x's OWN declaration is not a use of it. `function foo(...args: typeof
	// args) {}` names the parameter in its own type annotation, which is self-reference rather than
	// consumption, and upstream reports the parameter. Same judgment as a recursive function or a
	// self-referential type alias, arriving through a type query.
	case ast.KindTypeQuery:
		return typeQueryIsInsideItsOwnDeclaration(identifier)

	// The subject of a return type predicate names the parameter without reading it.
	// `function f(a: unknown): a is string { return true }` never touches `a` at runtime, and
	// upstream reports the parameter as unused while noting the predicate in its message. Counting
	// the predicate as a use makes every type guard's parameter permanently alive.
	//
	// Both spellings land here: `a is string` and `asserts a is string` are one node kind, with
	// `AssertsModifier` set on the second.
	case ast.KindTypePredicate:
		return parent.AsTypePredicateNode().ParameterName == identifier
	}

	return false
}

// exportDeclarationHasModuleSpecifier reports whether an export specifier's declaration re-exports
// from another module rather than exporting a local binding.
func exportDeclarationHasModuleSpecifier(specifier *ast.Node) bool {
	for current := specifier; current != nil; current = current.Parent {
		switch current.Kind {
		case ast.KindExportDeclaration:
			return current.AsExportDeclaration().ModuleSpecifier != nil
		case ast.KindSourceFile:
			return false
		}
	}
	return false
}

// typeQueryIsInsideItsOwnDeclaration reports whether a `typeof x` sits inside the declaration of x.
//
// See the call site: this is the self-reference judgment applied to a type query.
func typeQueryIsInsideItsOwnDeclaration(identifier *ast.Node) bool {
	name := identifier.Text()
	for current := identifier.Parent; current != nil; current = current.Parent {
		switch current.Kind {
		case ast.KindParameter, ast.KindVariableDeclaration:
			if declared := current.Name(); declared != nil &&
				declared.Kind == ast.KindIdentifier && declared.Text() == name {
				return true
			}
		case ast.KindSourceFile:
			return false
		}
	}
	return false
}

// resolveIdentifierSymbols resolves an identifier to every binding a read of it could keep alive.
//
// Usually one symbol. An export specifier is the exception and it needs both: `export { a }`
// resolves through `GetSymbolAtLocation` to the *export's* own symbol, which is not the symbol any
// local declaration carries, so recording only that leaves `const a = 1; export { a }` reporting a
// binding that is plainly exported. Following the alias reaches the local `const`, which fixes that
// case and breaks `import { a } from 'a'; export { a }`, because there the alias chain runs past the
// local import binding all the way to the foreign declaration.
//
// Both are upstream clean cases, so neither symbol alone is the answer and the union is. Recording
// an extra symbol is safe in the direction that matters: a symbol no candidate holds marks nothing,
// while a missing symbol is a false positive on correct code.
//
// A shorthand destructuring target resolves through the plain accessor to the *property's* symbol
// rather than to the value binding, which reads exactly like a correctly-declined shadow and is
// therefore silent in the expensive direction. The dedicated accessor is what gets `({a} = o)`
// right, and it lives in `services.go` rather than `checker.go`.
func resolveIdentifierSymbols(ctx rule.Context, identifier *ast.Node) []*ast.Symbol {
	if identifier.Parent != nil && identifier.Parent.Kind == ast.KindShorthandPropertyAssignment {
		if symbol := ctx.TypeChecker.GetShorthandAssignmentValueSymbol(identifier.Parent); symbol != nil {
			return []*ast.Symbol{symbol}
		}
	}

	symbol := ctx.TypeChecker.GetSymbolAtLocation(identifier)
	if symbol == nil {
		return nil
	}

	if identifier.Parent != nil && identifier.Parent.Kind == ast.KindExportSpecifier &&
		symbol.Flags&ast.SymbolFlagsAlias != 0 {
		if aliased := ctx.TypeChecker.GetAliasedSymbol(symbol); aliased != nil && aliased != symbol {
			return []*ast.Symbol{symbol, aliased}
		}
	}

	return []*ast.Symbol{symbol}
}

// collectCandidateBindings gathers every binding this rule may judge in one file.
//
// Anchored on declaration nodes rather than on a symbol table, because our substrate has no
// per-file symbol enumeration the way oxc's `scoping().symbol_ids()` does. The walk is the
// replacement for that enumeration, and it is why the kind list here has to be explicit: a
// declaration form nobody adds is a finding nobody reports, silently.
func collectCandidateBindings(sourceFile *ast.Node) []candidateBinding {
	var candidates []candidateBinding

	var visit func(*ast.Node)
	visit = func(current *ast.Node) {
		if current == nil {
			return
		}

		switch current.Kind {
		case ast.KindVariableDeclaration:
			// A catch binding is spelled `KindVariableDeclaration` in this AST and is ALSO reached
			// through its catch clause below, where it gets its own kind and its own ignore
			// pattern. Collecting it here as well reported it twice, which a span fixture caught
			// and a message-id fixture would not have: two findings with the right id at the right
			// place still read as correct until you count them.
			if current.Parent != nil && current.Parent.Kind == ast.KindCatchClause {
				break
			}
			// A binding pattern declares several names and each is judged separately, so the
			// pattern's own elements are reached by the walk rather than handled here.
			if name := current.Name(); name != nil && name.Kind == ast.KindIdentifier {
				candidates = append(candidates, candidateBinding{name, current, bindingVariable})
			}

		case ast.KindBindingElement:
			if name := current.Name(); name != nil && name.Kind == ast.KindIdentifier {
				candidates = append(candidates, candidateBinding{name, current, bindingVariable})
			}

		case ast.KindParameter:
			if name := current.Name(); name != nil && name.Kind == ast.KindIdentifier {
				candidates = append(candidates, candidateBinding{name, current, bindingParameter})
			}

		// `import X = Y` binds a local alias exactly like an import specifier does, and an unused
		// one is upstream's finding. It is a separate node kind from the module-import family, so
		// it needs its own arm rather than falling out of the one below.
		case ast.KindImportEqualsDeclaration:
			if name := current.Name(); name != nil && name.Kind == ast.KindIdentifier {
				candidates = append(candidates, candidateBinding{name, current, bindingImport})
			}

		case ast.KindImportSpecifier, ast.KindImportClause, ast.KindNamespaceImport:
			if name := current.Name(); name != nil && name.Kind == ast.KindIdentifier {
				candidates = append(candidates, candidateBinding{name, current, bindingImport})
			}

		// A named function or class declaration is a binding like any other, and upstream reports
		// an unused one: `function foo() {}` at file scope with nothing calling it is
		// `test_functions`' first failing case.
		case ast.KindFunctionDeclaration, ast.KindClassDeclaration:
			if name := current.Name(); name != nil && name.Kind == ast.KindIdentifier {
				candidates = append(candidates, candidateBinding{name, current, bindingVariable})
			}

		// Types, interfaces, enums and namespaces are checked exactly like values. Upstream states
		// this outright: "Enums and namespaces are treated the same as variables, classes,
		// functions, etc." An enum's MEMBERS are deliberately not candidates, because only the
		// enum itself is checked.
		case ast.KindInterfaceDeclaration,
			ast.KindTypeAliasDeclaration,
			ast.KindEnumDeclaration,
			ast.KindModuleDeclaration,
			ast.KindTypeParameter:
			if name := current.Name(); name != nil && name.Kind == ast.KindIdentifier {
				candidates = append(candidates, candidateBinding{name, current, bindingType})
			}

		case ast.KindCatchClause:
			if variable := current.AsCatchClause().VariableDeclaration; variable != nil {
				if name := variable.Name(); name != nil && name.Kind == ast.KindIdentifier {
					candidates = append(candidates, candidateBinding{name, variable, bindingCaughtError})
				}
			}
		}

		current.ForEachChild(func(child *ast.Node) bool {
			visit(child)
			return false
		})
	}
	visit(sourceFile)

	return candidates
}

// isExemptFromUnusedReport reports whether a binding nothing reads is nonetheless not a finding.
//
// Every arm here is an upstream exemption rather than a judgment of ours, and each one is the
// difference between a rule that is usable and one that reports every well-written file.
func isExemptFromUnusedReport(
	ctx rule.Context,
	candidate candidateBinding,
	all []candidateBinding,
	settings NoUnusedVarsOptions,
	reads map[*ast.Symbol]bool,
) bool {
	// The ignore pattern is checked before resolution because it is by far the cheapest test and it
	// settles the majority of real-tree candidates on its own.
	if matchesIgnorePattern(candidate, settings) {
		return true
	}

	// An import named `React` or `h` in a file containing JSX is used by the JSX transform, which
	// no source-level read expresses. Under the classic `"jsx": "react"` transform every element
	// compiles to `React.createElement`, so the import is load-bearing while looking untouched.
	//
	// Upstream carries this as an unconditional exemption at `mod.rs`'s `should_skip_symbol`, with
	// the comment that it cannot detect `jsxPragma` or tell `react` from `react-jsx`, so it allows
	// all cases rather than guessing. This reproduces that decision including its breadth.
	//
	// Measured, and it is the reason this guard is here rather than in a later pass: without it
	// this rule reported 512 findings on our tree and roughly 470 of them were `import React from
	// 'react'` at the top of a `.tsx` file. Every one was wrong, and every one looked exactly like
	// a genuine unused import in the output.
	if candidate.kind == bindingImport && isJsxFactoryImportInJsxFile(candidate) {
		return true
	}

	// A parameter under `args: "none"` is never reported. Under the default `after-used`, only
	// parameters following the last used one are.
	if candidate.kind == bindingParameter {
		if settings.Args == "none" {
			return true
		}
		// A setter's parameter cannot be removed: `set foo() {}` is a syntax error. Upstream
		// exempts it for exactly that reason at `allowed.rs`'s
		// `is_allowed_param_because_of_method`, and so does a constructor parameter carrying an
		// accessibility modifier, which declares a class member rather than a mere argument.
		if isStructurallyRequiredParameter(candidate.declaration) {
			return true
		}
		if settings.Args != "all" && isParameterBeforeAUsedOne(ctx, candidate, all, reads) {
			return true
		}
	}

	// A mapped type's key is always used by the type it builds, and it has no other spelling.
	// `{ [K in Slots]?: string }` binds `K` and the value position may legitimately ignore it, as
	// in `{ [K in Slots]?: React.ReactNode }`. Upstream states this as a bare no-op arm at
	// `mod.rs:388` with the comment "Mapped type keys are always used within the type definition".
	//
	// This is a deliberate divergence from ESLint, which DOES report it: measured with ESLint's own
	// Linter API, `type S<T extends string> = { [K in T]?: string }` reports `K` as unused_code_report. oxc is
	// the differential authority, so oxc's answer ships, and the tree agrees with oxc: the one
	// occurrence in our own source is a correct mapped type that ESLint would have flagged.
	if candidate.kind == bindingType && candidate.declaration.Parent != nil &&
		candidate.declaration.Parent.Kind == ast.KindMappedType {
		return true
	}

	// A `this` parameter declares the receiver's type and is not a binding anyone can delete: it
	// occupies no argument position, so removing it changes the signature's meaning rather than
	// tidying it. ESLint reports it, measured; oxc never collects it as a symbol at all because its
	// parser models `this` separately from formal parameters. Same answer, reached differently.
	if candidate.kind == bindingParameter && candidate.name.Text() == "this" {
		return true
	}

	// A declaration with no body declares a signature rather than defining anything, so its
	// parameter names are documentation and removing them changes no behavior. Overload
	// signatures, ambient declarations, interface methods and abstract members all land here.
	if isInsideSignatureOrAmbientDeclaration(candidate.declaration) {
		return true
	}

	// A namespace written with a dotted name (`namespace foo.bar {}`) parses as nested module
	// declarations, and only the innermost one carries the body. The outer segments are path, not
	// bindings anybody could use, so reporting them accuses the author of not using syntax that
	// has no other spelling.
	if candidate.declaration.Kind == ast.KindModuleDeclaration &&
		candidate.declaration.Body() != nil &&
		candidate.declaration.Body().Kind == ast.KindModuleDeclaration {
		return true
	}

	symbol := ctx.TypeChecker.GetSymbolAtLocation(candidate.name)
	if symbol == nil {
		// A name the checker cannot resolve is not provably unused_code_report. Reporting it would be a guess
		// in the direction that costs a false positive on code that is very likely fine.
		return true
	}

	if reads[symbol] {
		return true
	}

	// An imported name is an alias, and a read reached through a different route can land on the
	// declaration it aliases rather than on the local binding. `import { a } from 'a'; export { a }`
	// is upstream's clean case and both halves resolve to a symbol spelled `a` that is not the same
	// symbol, so the equality above misses it. Asking the alias question from the candidate's side
	// closes that gap without widening what a read means.
	//
	// An `import X = Y` is excluded, and this is the one place the two alias forms must be told
	// apart. There the aliased symbol is a LOCAL binding in the same file, so a read of `Y`
	// anywhere — including the `= Y` in the alias's own declaration — would count as a read of `X`
	// and keep every such alias permanently alive. A module import has no such hazard because the
	// symbol it aliases lives in another file that this rule never reads.
	if symbol.Flags&ast.SymbolFlagsAlias != 0 &&
		candidate.declaration.Kind != ast.KindImportEqualsDeclaration {
		if aliased := ctx.TypeChecker.GetAliasedSymbol(symbol); aliased != nil && reads[aliased] {
			return true
		}
	}

	// A `for (x in o)` or `for (x of it)` whose body's first statement is `return` marks the loop
	// variable used. The loop exists for its side effect, proving the collection is non-empty, and
	// the binding is the syntax that expresses it, so upstream declines to report it.
	//
	// This looks arbitrary and it is deliberately narrow: only a leading `return` counts, so
	// `for (const k in o) { log(k) }` is decided by the ordinary read test and
	// `for (const k in o) { sideEffect() }` still reports. Upstream's `is_used_in_for_of_loop` at
	// `usage.rs:203` is the whole specification and this reproduces its shape rather than
	// generalizing it.
	if isForInOrOfBindingWithLeadingReturn(candidate.declaration) ||
		hasForInOrOfWriteWithLeadingReturn(ctx, candidate, symbol) {
		return true
	}

	// An exported binding is read by a file this rule cannot see. See the rule's doc comment for
	// why that stays an assumption rather than becoming a program-wide lookup.
	//
	// EVERY declaration of the symbol is asked, not just the one this candidate came from, because
	// declaration merging can put the `export` on a sibling: `interface Foo {}` beside
	// `export const Foo = 'bar'` is one exported symbol wearing two declarations, and upstream
	// treats it as exported. Both orderings appear in the corpus as clean cases, which is what
	// forced the loop; asking only `candidate.declaration` reported the interface in both.
	//
	// A loop rather than an index, and the reason is the question being asked: "does ANY
	// declaration export this", where more declarations only mean more chances to match. An
	// index-zero test goes silent whenever the exporting declaration is not written first, and the
	// corpus pins exactly that ordering.
	for _, declaration := range symbol.Declarations {
		if isExportedBinding(declaration) {
			return true
		}
	}
	if isExportedBinding(candidate.declaration) {
		return true
	}

	return false
}

// isReadInsideAClosureAssignedToItself reports whether a read sits inside a function expression
// that is being assigned to the very binding being read.
//
// See the call site for the two neighbouring shapes that must NOT match, and why.
func isReadInsideAClosureAssignedToItself(identifier *ast.Node) bool {
	name := identifier.Text()

	for current := identifier.Parent; current != nil; current = current.Parent {
		switch current.Kind {
		// A read inside a parameter's own default belongs to the closure's parameter scope, where
		// the parameter shadows anything outside spelled the same. `let a; a = function(a = a) {};`
		// is upstream's clean case and the inner `a` is the parameter, not the outer binding, so
		// climbing past this reported a variable that is genuinely used.
		case ast.KindParameter:
			return false

		case ast.KindFunctionExpression, ast.KindArrowFunction:
			// The closure must be the DIRECT right-hand side of the assignment. A call wrapping it
			// means it runs immediately, and a sequence means the assigned value is something
			// else; both are upstream passing cases.
			parent := current.Parent
			if parent == nil || parent.Kind != ast.KindBinaryExpression {
				return false
			}
			binary := parent.AsBinaryExpression()
			if binary.OperatorToken == nil ||
				binary.OperatorToken.Kind != ast.KindEqualsToken || binary.Right != current {
				return false
			}
			target := unwrapAssignmentTarget(binary.Left)
			return target != nil && target.Kind == ast.KindIdentifier && target.Text() == name

		case ast.KindFunctionDeclaration, ast.KindMethodDeclaration, ast.KindSourceFile:
			return false
		}
	}

	return false
}

// isDiscardedSequenceOperandRead reports whether a read sits in a comma sequence position whose
// value nothing can observe.
//
// See the call site for why only the left operand counts.
func isDiscardedSequenceOperandRead(identifier *ast.Node) bool {
	current := identifier
	for current != nil && current.Parent != nil {
		parent := current.Parent
		switch parent.Kind {
		case ast.KindParenthesizedExpression:
			current = parent

		case ast.KindBinaryExpression:
			binary := parent.AsBinaryExpression()
			if binary.OperatorToken == nil || binary.OperatorToken.Kind != ast.KindCommaToken {
				return false
			}
			if binary.Left == current {
				return true
			}
			// The right operand is the sequence's value, so it inherits the sequence's own fate
			// and the walk continues rather than answering here.
			current = parent

		default:
			return false
		}
	}
	return false
}

// isSelfReassignmentRead reports whether a read of a binding exists only to write that same binding.
//
// The shape is `a = a + 1`, `a += 1`, `a++`. Each loads the binding, so the load is a genuine read
// in the dataflow sense, and upstream nonetheless declines to count it: the value goes nowhere but
// back into the same name, so no observer of the program can tell whether the binding exists.
//
// Two escapes make the read count again, and both matter on real code:
//
// A different variable scope. `let cancel = () => {}; function f(){ cancel = cancel?.(); }` is
// clean upstream, because the assignment sits in a new function scope and the value can be observed
// on a later call. The same statement in a plain block is a finding. That is `usage.rs:503`'s
// variable-scope comparison, and the corpus pins both halves in `test_vars_self_use`.
//
// A loop. `for (let i = 0; i < 10; i++) { chain = chain.extend(); }` is clean, because the back
// edge makes each store observable by the next iteration's load. Outside a loop the same statement
// reports. This is the case a liveness dataflow would answer differently, and the rule's doc
// comment records why upstream's syntactic answer is reproduced instead.
func isSelfReassignmentRead(identifier *ast.Node, declaringScope *ast.Node) bool {
	name := identifier.Text()

	for current := identifier; current != nil && current.Parent != nil; current = current.Parent {
		parent := current.Parent

		switch parent.Kind {
		// Transparent wrappers on the way out to the operation that decides.
		case ast.KindParenthesizedExpression,
			ast.KindPropertyAccessExpression,
			ast.KindElementAccessExpression,
			ast.KindNonNullExpression,
			ast.KindAsExpression,
			ast.KindCallExpression:
			// A call whose ARGUMENTS contain the reference is a genuine use: the value escapes
			// into the callee. Only the callee position stays transparent, which is what keeps
			// `cancel = cancel?.()` self-referential while `f(a)` is not.
			if parent.Kind == ast.KindCallExpression &&
				parent.AsCallExpression().Expression != current {
				return false
			}
			continue

		// A read feeding another binding's declaration or a property definition escapes, so the
		// value is observable and this is a real use.
		case ast.KindVariableDeclaration, ast.KindPropertyDeclaration, ast.KindJsxExpression:
			return false

		case ast.KindPrefixUnaryExpression:
			if !reference.IsUpdateOperator(parent.AsPrefixUnaryExpression().Operator) {
				return false
			}
			if current != identifier {
				// The update targets a property reached through this binding: `acc[k]++` writes
				// to the object, so `acc` itself is genuinely read. Upstream notes this at
				// `usage.rs:734` and separates it from `(a++, 0)` by exactly this test.
				return false
			}
			return isDiscardedResult(parent) && !isInsideLoop(parent)

		case ast.KindPostfixUnaryExpression:
			if !reference.IsUpdateOperator(parent.AsPostfixUnaryExpression().Operator) {
				return false
			}
			if current != identifier {
				return false
			}
			return isDiscardedResult(parent) && !isInsideLoop(parent)

		case ast.KindBinaryExpression:
			binary := parent.AsBinaryExpression()
			if binary.OperatorToken == nil || !ast.IsAssignmentOperator(binary.OperatorToken.Kind) {
				// An ordinary operator such as `+` is transparent: `a = a + 1` reaches the
				// assignment through it, and that is the shape this whole function is about.
				continue
			}
			// The reference sits on the right of an assignment. It is self-referential only when
			// the target is the same name, in the same variable scope, outside a loop.
			target := unwrapAssignmentTarget(binary.Left)
			if target == nil || target.Kind != ast.KindIdentifier || target.Text() != name {
				return false
			}
			// Upstream compares the REFERENCE's variable scope against the BINDING's declaring
			// scope, not the two identifiers against each other: those are always in the same
			// scope as one another, so that comparison is vacuously true and the exemption never
			// fires. `let cancel = () => {}; export function close() { cancel = cancel?.() }` is
			// upstream's clean case and it is clean precisely because the assignment sits in a new
			// function scope where the stored value can be observed on a later call. The same
			// statement in a plain block reports.
			if enclosingVariableScope(identifier) == declaringScope && isDiscardedResult(parent) &&
				!isInsideLoop(parent) {
				return true
			}
			return false

		default:
			return false
		}
	}

	return false
}

// isDiscardedResult reports whether an expression's value goes nowhere.
//
// This is what separates `a++` from `b = a++`. Both load `a`, but only the second lets anything
// observe the loaded value, and upstream counts only the second as a use. Its name for this is
// `is_discarded_read` at `usage.rs:709`.
//
// An expression's result is discarded when it sits directly as a statement, or as a non-final
// element of a comma sequence whose own result is discarded. Everything else — an initializer, an
// argument, an operand, a return value, a condition — consumes it.
func isDiscardedResult(expression *ast.Node) bool {
	current := expression
	for current != nil && current.Parent != nil {
		parent := current.Parent
		switch parent.Kind {
		case ast.KindParenthesizedExpression:
			current = parent

		case ast.KindExpressionStatement:
			return true

		// A cast is transparent to where the value goes. `a = a++ as any` discards through the
		// `as` exactly as `a = a++` does, and upstream reports both.
		case ast.KindAsExpression, ast.KindNonNullExpression, ast.KindSatisfiesExpression:
			current = parent

		case ast.KindBinaryExpression:
			binary := parent.AsBinaryExpression()
			// A comma sequence discards every operand but the last, and the last inherits the
			// sequence's own fate. `(a++, 0)` discards `a++`; `(0, x++)` does not, because the
			// update is the value the sequence yields.
			if binary.OperatorToken != nil && binary.OperatorToken.Kind == ast.KindCommaToken {
				if binary.Left == current {
					return true
				}
				current = parent
				continue
			}
			// An assignment feeding the value straight back into the SAME binding discards it as
			// surely as a bare statement does: `a = ++a` and `a = (0, ++a)` leave nothing any
			// reader can observe, and upstream reports both. The target has to be the same name,
			// otherwise `b = a++` is a genuine escape into `b`.
			if binary.OperatorToken != nil && ast.IsAssignmentOperator(binary.OperatorToken.Kind) &&
				binary.Right == current {
				target := unwrapAssignmentTarget(binary.Left)
				if target != nil && target.Kind == ast.KindIdentifier &&
					target.Text() == updatedBindingName(expression) {
					current = parent
					continue
				}
			}
			return false

		// A `for` head's initializer and incrementor are evaluated for effect, so a value there is
		// discarded. The test is not: the loop reads it.
		case ast.KindForStatement:
			forStatement := parent.AsForStatement()
			return forStatement.Initializer == current || forStatement.Incrementor == current

		default:
			return false
		}
	}
	return false
}

// updatedBindingName returns the name an update expression writes to, or the empty string.
//
// Used to tell `a = ++a` from `b = ++a`: the first discards the value back into the binding it came
// from and the second lets `b` observe it.
func updatedBindingName(expression *ast.Node) string {
	var operand *ast.Node
	switch expression.Kind {
	case ast.KindPrefixUnaryExpression:
		operand = expression.AsPrefixUnaryExpression().Operand
	case ast.KindPostfixUnaryExpression:
		operand = expression.AsPostfixUnaryExpression().Operand
	case ast.KindBinaryExpression:
		operand = expression.AsBinaryExpression().Left
	default:
		return ""
	}
	if operand = unwrapAssignmentTarget(operand); operand != nil &&
		operand.Kind == ast.KindIdentifier {
		return operand.Text()
	}
	return ""
}

// unwrapAssignmentTarget strips the wrappers an assignment target can wear.
func unwrapAssignmentTarget(node *ast.Node) *ast.Node {
	for node != nil {
		switch node.Kind {
		case ast.KindParenthesizedExpression:
			node = node.AsParenthesizedExpression().Expression
		case ast.KindNonNullExpression:
			node = node.AsNonNullExpression().Expression
		case ast.KindAsExpression:
			node = node.AsAsExpression().Expression
		default:
			return node
		}
	}
	return nil
}

// sameVariableScope reports whether two nodes sit in the same function-level scope.
//
// Function-level rather than block-level, deliberately: upstream compares *variable* scopes, so a
// plain block does not separate a binding from a write to it while a function does. That is the
// difference between `{ cancel = cancel?.() }` reporting and `function f(){ cancel = cancel?.() }`
// staying clean, both of which the corpus pins.
func sameVariableScope(first *ast.Node, second *ast.Node) bool {
	return enclosingVariableScope(first) == enclosingVariableScope(second)
}

// enclosingVariableScope returns the nearest function, module, or class static block containing a
// node, or nil at file scope.
func enclosingVariableScope(node *ast.Node) *ast.Node {
	for current := node; current != nil; current = current.Parent {
		switch current.Kind {
		case ast.KindFunctionDeclaration,
			ast.KindFunctionExpression,
			ast.KindArrowFunction,
			ast.KindMethodDeclaration,
			ast.KindGetAccessor,
			ast.KindSetAccessor,
			ast.KindConstructor,
			ast.KindClassStaticBlockDeclaration,
			ast.KindModuleDeclaration,
			ast.KindSourceFile:
			return current
		}
	}
	return nil
}

// isInsideLoop reports whether a node sits in a loop's body, test, or update.
//
// A loop's back edge is what makes a self-assignment observable: the store this iteration performs
// is loaded by the next one. Upstream reaches the same conclusion through two separate predicates,
// `is_in_for_loop_test_or_update` and `is_in_loop_body`, and both are collapsed here because the
// answer they feed is the same.
//
// The walk stops at a function boundary. A loop outside a closure does not make an assignment
// inside that closure observable, since the closure's body runs when it is called rather than on
// each iteration.
func isInsideLoop(node *ast.Node) bool {
	for current := node; current != nil; current = current.Parent {
		switch current.Kind {
		case ast.KindForStatement,
			ast.KindForInStatement,
			ast.KindForOfStatement,
			ast.KindWhileStatement,
			ast.KindDoStatement:
			return true
		case ast.KindFunctionDeclaration,
			ast.KindFunctionExpression,
			ast.KindArrowFunction,
			ast.KindMethodDeclaration,
			ast.KindSourceFile:
			return false
		}
	}
	return false
}

// isSelfReferenceWithinOwnDeclaration reports whether an identifier refers to the very function or
// class that lexically contains it.
//
// `function foox() { return foox(); }` is upstream's own failing case: a recursive function nothing
// else calls is unreachable, and counting the recursive call as a use is what would keep an entire
// abandoned call graph looking alive. This tree's `internal/unused` package reaches the identical
// verdict from an argument rather than from a corpus; this is the corpus confirming it.
//
// The comparison is on SYMBOL identity rather than on the spelled name, and that is load-bearing
// rather than tidy. `function foo() { var foo = 1; return foo }` returns the inner variable, not the
// function, and a name comparison calls it a self-reference and reports a variable upstream calls
// clean. The same shape arrives as a shadowing parameter in `function foo(foo) { return foo }`.
// Both are upstream passing cases and both were false positives here until identity replaced text.
func isSelfReferenceWithinOwnDeclaration(ctx rule.Context, identifier *ast.Node, symbol *ast.Symbol) bool {
	for current := identifier.Parent; current != nil; current = current.Parent {
		switch current.Kind {
		// A type that only appears in its own definition is not used. `type Foo = Array<Foo>` and
		// `interface LinkedList<T> { next: LinkedList<T> }` are upstream's own failing cases, and
		// its documentation states the rule outright: "A type or interface is not considered to be
		// used if it is only ever used in its own definition." Same judgment as the recursive
		// function below, reached for the same reason.
		case ast.KindTypeAliasDeclaration,
			ast.KindInterfaceDeclaration:
			if declarationDeclaresSymbol(ctx, current.Name(), symbol) {
				return true
			}

		case ast.KindFunctionDeclaration,
			ast.KindFunctionExpression,
			ast.KindClassDeclaration,
			ast.KindClassExpression,
			ast.KindArrowFunction:
			if declarationDeclaresSymbol(ctx, current.Name(), symbol) {
				return true
			}
			// A variable declarator holding a function expression names it: `const foo = () => {
			// return foo }` is upstream's second self-use failure, and the arrow is anonymous, so
			// the name has to come from the declarator.
			if current.Parent != nil && current.Parent.Kind == ast.KindVariableDeclaration &&
				declarationDeclaresSymbol(ctx, current.Parent.Name(), symbol) {
				return true
			}

		case ast.KindSourceFile:
			return false
		}
	}

	return false
}

// declarationDeclaresSymbol reports whether a declaration's name node binds the given symbol.
func declarationDeclaresSymbol(ctx rule.Context, name *ast.Node, symbol *ast.Symbol) bool {
	if name == nil || name.Kind != ast.KindIdentifier {
		return false
	}
	return ctx.TypeChecker.GetSymbolAtLocation(name) == symbol
}

// isForInOrOfBindingWithLeadingReturn reports whether a binding is the loop variable of a
// `for...in` or `for...of` whose body returns immediately.
//
// See the call site for why this exemption exists and why it is not generalized.
func isForInOrOfBindingWithLeadingReturn(declaration *ast.Node) bool {
	for current := declaration; current != nil; current = current.Parent {
		switch current.Kind {
		case ast.KindForInStatement, ast.KindForOfStatement:
			// The typed accessor rather than `Node.Body()`. Measured: `Body()` returns nil on both
			// loop kinds, so the exemption below silently never fired and sixteen of upstream's
			// clean cases reported. A nil from a convenience accessor reads exactly like a loop
			// with no body, which is why this cost a full measurement cycle to find.
			body := current.AsForInOrOfStatement().Statement
			if body == nil {
				return false
			}
			if body.Kind == ast.KindReturnStatement {
				return true
			}
			if body.Kind == ast.KindBlock {
				statements := body.Statements()
				return len(statements) > 0 && statements[0].Kind == ast.KindReturnStatement
			}
			return false

		// A function boundary means the loop, if any, is outside the binding's own scope.
		case ast.KindFunctionDeclaration,
			ast.KindFunctionExpression,
			ast.KindArrowFunction,
			ast.KindSourceFile:
			return false
		}
	}
	return false
}

// isJsxFactoryImportInJsxFile reports whether a binding is a JSX factory import in a file using JSX.
//
// See the call site for why the name test is `React` or `h` specifically and why the breadth is
// upstream's rather than ours.
func isJsxFactoryImportInJsxFile(candidate candidateBinding) bool {
	name := candidate.name.Text()
	if name != "React" && name != "h" {
		return false
	}

	sourceFile := ast.GetSourceFileOfNode(candidate.declaration)
	if sourceFile == nil {
		return false
	}
	return strings.HasSuffix(sourceFile.FileName(), ".tsx") ||
		strings.HasSuffix(sourceFile.FileName(), ".jsx")
}

// hasForInOrOfWriteWithLeadingReturn covers the same exemption for a binding declared apart from
// the loop that assigns it.
//
// `var name; for (name in obj) return;` binds the loop variable by assignment rather than in the
// head, so the declaration has no `ForInStatement` above it and the climb in the sibling helper
// cannot see the loop. Both spellings appear in upstream's corpus, twelve cases apart, and they are
// the same judgment reached from two directions.
func hasForInOrOfWriteWithLeadingReturn(ctx rule.Context, candidate candidateBinding, symbol *ast.Symbol) bool {
	sourceFile := ast.GetSourceFileOfNode(candidate.declaration)
	if sourceFile == nil {
		return false
	}

	name := candidate.name.Text()
	found := false

	var visit func(*ast.Node)
	visit = func(current *ast.Node) {
		if current == nil || found {
			return
		}
		if current.Kind == ast.KindIdentifier && current.Text() == name &&
			isForInOrOfBindingWithLeadingReturn(current) &&
			ctx.TypeChecker.GetSymbolAtLocation(current) == symbol {
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

// isStructurallyRequiredParameter reports whether a parameter cannot be deleted without changing
// what the surrounding code means.
//
// Two shapes. A setter must take exactly one parameter, so an unused one is required by the
// grammar. And a constructor parameter carrying `public`, `private`, `protected` or `readonly`
// declares a class property, so it is doing work even when the constructor body never reads it.
func isStructurallyRequiredParameter(parameter *ast.Node) bool {
	owner := parameter.Parent
	if owner == nil {
		return false
	}

	if owner.Kind == ast.KindSetAccessor {
		return true
	}

	// An `override` method's signature is fixed by the base class it overrides, so an unused
	// parameter there is required by the contract rather than left behind. Upstream reaches this
	// through the same `is_allowed_param_because_of_method` family.
	if owner.Kind == ast.KindMethodDeclaration {
		if modifiers := owner.Modifiers(); modifiers != nil {
			for _, modifier := range modifiers.Nodes {
				if modifier.Kind == ast.KindOverrideKeyword {
					return true
				}
			}
		}
	}

	if owner.Kind == ast.KindConstructor {
		if modifiers := parameter.Modifiers(); modifiers != nil {
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
	}

	return false
}

// isAmbientInterfaceMember reports whether a declaration belongs to an interface.
//
// See the ambient-block reasoning in isInsideSignatureOrAmbientDeclaration for why an interface is
// treated differently from a type alias or a class in the same position.
func isAmbientInterfaceMember(declaration *ast.Node) bool {
	for current := declaration; current != nil; current = current.Parent {
		switch current.Kind {
		case ast.KindInterfaceDeclaration:
			return true
		case ast.KindModuleBlock, ast.KindSourceFile:
			return false
		}
	}
	return false
}

// enclosingAmbientModuleBlock returns the module block a node sits in, or nil.
func enclosingAmbientModuleBlock(node *ast.Node) *ast.Node {
	for current := node; current != nil; current = current.Parent {
		switch current.Kind {
		case ast.KindModuleBlock:
			return current
		case ast.KindSourceFile:
			return nil
		}
	}
	return nil
}

// moduleBlockHasExplicitExports reports whether a module block states an interface of its own.
//
// An `export` modifier on a member, an `export {}` or `export ... from`, or an `export =` all
// count. See the call site for why the distinction decides whether the block's contents are judged.
func moduleBlockHasExplicitExports(block *ast.Node) bool {
	for _, statement := range block.Statements() {
		switch statement.Kind {
		case ast.KindExportDeclaration, ast.KindExportAssignment:
			return true
		}
		if hasExportModifier(statement) {
			return true
		}
	}
	return false
}

// isInsideSignatureOrAmbientDeclaration reports whether a binding belongs to a declaration that
// declares a shape rather than defining a value.
//
// An overload signature, an interface method, an abstract member, and anything under `declare` all
// name their parameters for the reader's benefit; nothing runs, so nothing can use them. Upstream
// reaches the same answer through `formal_parameters.kind.is_signature()` at `mod.rs`'s
// `should_skip_symbol`.
func isInsideSignatureOrAmbientDeclaration(declaration *ast.Node) bool {
	for current := declaration; current != nil; current = current.Parent {
		if ast.GetCombinedNodeFlags(current)&ast.NodeFlagsAmbient != 0 {
			// An ambient module exempts its contents only when it declares NO explicit exports.
			// `declare module 'm' { type T = any; }` has no export, so everything in it is part of
			// the ambient surface and nothing is judged. Add one export and the block becomes a
			// module with a stated interface, so upstream judges the rest of its contents against
			// that interface: `declare module 'foo' { type Test = any; const x = 1; export = x; }`
			// reports `Test`.
			//
			// Upstream states this as `is_ambient_external_module_without_explicit_exports` at
			// `allowed.rs:93`, and the same test governs ambient namespaces at `:105`. Reading the
			// ambient flag alone exempted both shapes and cost four of this port's missed cases.
			if block := enclosingAmbientModuleBlock(current); block != nil &&
				moduleBlockHasExplicitExports(block) {
				return false
			}
			// An ambient block still gets its type-alias and class type parameters judged, while
			// an interface's are left alone. That split is upstream's and it is visible only in
			// the snapshot: `declare module 'bun:test' { type Matchers2<T> = {} }` reports `T` and
			// `{ class MyClass<T> {} }` reports both the class and `T`, while
			// `declare module 'vitest' { interface Matchers<T> {...} }` is CLEAN.
			//
			// The reason is what an interface in an ambient module is for. It is the declaration
			// merging idiom: a library augments a type it does not own, and the type parameter is
			// dictated by the interface being merged into rather than chosen here. A type alias or
			// a class in the same position declares something new, so an unused parameter on it is
			// the author's own loose end.
			if isAmbientInterfaceMember(declaration) {
				return true
			}
			if declaration.Kind == ast.KindTypeParameter {
				return false
			}
			return true
		}

		switch current.Kind {
		case ast.KindMethodSignature,
			ast.KindCallSignature,
			ast.KindConstructSignature,
			ast.KindIndexSignature,
			ast.KindFunctionType,
			ast.KindConstructorType:
			return true

		case ast.KindFunctionDeclaration,
			ast.KindMethodDeclaration,
			ast.KindConstructor:
			// A body-less declaration is an overload signature or an abstract member, and its
			// parameter names are documentation. But that only holds when the declaration IS the
			// thing being judged or encloses it: `export namespace N { function foo() }` has a
			// body-less `foo` whose own NAME is the candidate, and upstream reports it. The
			// signature exemption is about what a signature's parameters mean, not about the
			// signature's own name.
			if current == declaration {
				return false
			}
			return current.Body() == nil

		case ast.KindSourceFile:
			return false
		}
	}
	return false
}

// isParameterBeforeAUsedOne implements the default `args: "after-used"` behavior.
//
// A parameter can only be removed if every parameter after it is also removable, because argument
// position is the calling convention: deleting the second of three parameters silently reassigns
// the third. So upstream reports only the trailing run of unused parameters, and this reproduces
// that by asking whether any later parameter in the same list is read.
func isParameterBeforeAUsedOne(
	ctx rule.Context,
	candidate candidateBinding,
	all []candidateBinding,
	reads map[*ast.Symbol]bool,
) bool {
	owner := candidate.declaration.Parent
	if owner == nil {
		return false
	}

	seenSelf := false
	for _, other := range all {
		if other.declaration == candidate.declaration {
			seenSelf = true
			continue
		}
		if !seenSelf || other.kind != bindingParameter || other.declaration.Parent != owner {
			continue
		}
		// A later parameter carrying a modifier counts as used even when nothing reads it, because
		// it declares a class property and so cannot be removed. Upstream says this at the line:
		// "has_modifier() to handle: constructor(unused: number, public property: string) {}".
		// Without it, `constructor(baz: string, private logger: Logger)` reports `baz`, which is a
		// parameter that genuinely cannot be deleted without breaking the property behind it.
		if isStructurallyRequiredParameter(other.declaration) {
			return true
		}
		if symbol := ctx.TypeChecker.GetSymbolAtLocation(other.name); symbol != nil && reads[symbol] {
			return true
		}
	}

	return false
}

// isExportedBinding reports whether a declaration leaves the file.
//
// Climbing rather than testing the declaration itself, because the export modifier sits on the
// statement while the binding sits several nodes down: `export const { a, b } = o;` puts `export`
// on the variable statement and `a` inside a binding pattern inside a declaration list.
func isExportedBinding(declaration *ast.Node) bool {
	for current := declaration; current != nil; current = current.Parent {
		switch current.Kind {
		// A parameter of an exported function is NOT exported. The function leaves the file; its
		// parameter list does not, and no importer can read a parameter name. Climbing past these
		// exempted every parameter of every exported function, which was 18 of this port's 35
		// missed cases and the largest single group of them.
		//
		// The boundary is the declaration that OWNS a parameter list or a body, rather than a
		// scope boundary generally: `export const f = () => { const inner = 1; }` must still stop
		// at the arrow, because `inner` is not exported either.
		// A parameter is never exported, whatever encloses it. The function leaves the file; its
		// parameter list does not, and no importer can read a parameter name. This is checked on
		// the parameter ITSELF rather than only on the way past one, because a parameter is where
		// the climb starts and a `current != declaration` guard would let it walk straight up to
		// the exported function it belongs to. That was the bug in the first attempt at this, and
		// it read as the narrowing simply having no effect.
		case ast.KindParameter:
			return false

		// A TYPE PARAMETER is never exported either, for the same reason and with the same
		// mechanics: `export interface M<T> {}` exports `M`, and no importer can name `T`. It has
		// to answer on the node ITSELF rather than on the way past one, because a type parameter is
		// where the climb starts. Upstream reports `T` here and reports the `R` bound by an `infer`
		// in an exported conditional type, both of which stayed silent while the climb walked up to
		// the exported declaration.
		case ast.KindTypeParameter:
			return false

		// The declarations that own a body. Anything reached THROUGH one of these is inside a
		// scope rather than at the top of an exported statement, so it does not leave the file:
		// `export const f = () => { const inner = 1; }` exports `f` and not `inner`. The
		// `current != declaration` guard is what lets the declaration itself still be judged, since
		// `export function f() {}` is a genuine export of `f`.
		case ast.KindFunctionExpression,
			ast.KindArrowFunction,
			ast.KindMethodDeclaration,
			ast.KindConstructor,
			ast.KindGetAccessor,
			ast.KindSetAccessor,
			ast.KindBlock,
			// A namespace body is a scope, not a re-export. `export namespace N { function foo() }`
			// exports `N`; `foo` stays inside and upstream reports it. Only a declaration carrying
			// its own `export` inside the namespace leaves, and that is caught by the modifier test
			// below on the inner declaration itself rather than by the climb.
			ast.KindModuleBlock:
			if current != declaration {
				return false
			}

		// An `import X = Y` leaves the file only when it carries its own `export`. Sitting inside an
		// exported namespace does not export it: `export namespace Bar { import TheFoo = Foo; }`
		// exports `Bar`, and `TheFoo` is a local alias upstream reports. Answering here rather than
		// continuing the climb is what separates the two.
		case ast.KindImportEqualsDeclaration:
			return hasExportModifier(current)

		case ast.KindVariableStatement,
			ast.KindFunctionDeclaration,
			ast.KindClassDeclaration,
			ast.KindInterfaceDeclaration,
			ast.KindTypeAliasDeclaration,
			ast.KindEnumDeclaration,
			ast.KindModuleDeclaration:
			if hasExportModifier(current) {
				return true
			}
		case ast.KindSourceFile:
			return false
		}
	}
	return false
}

// hasExportModifier reports whether a statement carries `export` or `export default`.
func hasExportModifier(node *ast.Node) bool {
	modifiers := node.Modifiers()
	if modifiers == nil {
		return false
	}
	for _, modifier := range modifiers.Nodes {
		if modifier.Kind == ast.KindExportKeyword || modifier.Kind == ast.KindDefaultKeyword {
			return true
		}
	}
	return false
}

// matchesIgnorePattern reports whether a binding's name says the omission is deliberate.
//
// The default is a leading underscore for every kind, which is oxc's answer and NOT ESLint's — see
// the rule's doc comment, where both are measured. The one asymmetry: a parameter named exactly `_`
// is still reported under the default, while a variable named `_` is ignored. That is upstream's
// special case at `ignored.rs:424` and it is reproduced rather than smoothed over.
func matchesIgnorePattern(candidate candidateBinding, settings NoUnusedVarsOptions) bool {
	name := candidate.name.Text()

	pattern := settings.VarsIgnorePattern
	isDefaultPattern := settings.varsPatternIsDefault

	switch candidate.kind {
	case bindingParameter:
		pattern, isDefaultPattern = settings.ArgsIgnorePattern, settings.argsPatternIsDefault
		if isDefaultPattern && name == "_" {
			return false
		}
	case bindingCaughtError:
		// A caught error has NO default ignore pattern, unlike a variable or a parameter. Upstream
		// defaults `caughtErrorsIgnorePattern` to `IgnorePattern::None` at `options.rs:378` while
		// the other two default to `IgnorePattern::Default`, so `try {} catch(_) {}` reports.
		//
		// This port applied the underscore default to all three and recorded the asymmetry as a
		// deliberate gap, on the reasoning that our tree has many `catch (_)`. Measured rather than
		// assumed: the tree has none that this rule reaches, so reproducing upstream costs nothing
		// here and the gap was being paid for a cost that did not exist.
		if settings.CaughtErrorsIgnorePattern == "" {
			return false
		}
		pattern, isDefaultPattern = settings.CaughtErrorsIgnorePattern, false
	}

	if isDefaultPattern {
		return strings.HasPrefix(name, "_")
	}
	if pattern == "" {
		return false
	}

	compiled, err := regexp.Compile(pattern)
	if err != nil {
		// An unparseable pattern ignores nothing rather than everything. The other direction would
		// silence the whole rule on a typo in a config file, which is the failure that looks clean.
		return false
	}
	return compiled.MatchString(name)
}

// NoUnusedVarsOptions is the rule's configuration surface.
//
// Every field and every default is taken from oxc's `options.rs` rather than from the inventory,
// which records this rule as having no options and is wrong: the option module alone is 891 lines.
// The defaults that are not the zero value are the ones worth pinning with a fixture, and three of
// them are not: the ignore patterns default to a leading underscore rather than to nothing, `args`
// defaults to `after-used` rather than to `all`, and `caughtErrors` defaults to `all`.
type NoUnusedVarsOptions struct {
	// Vars is `all` or `local`. Default `all`.
	Vars string `json:"vars"`
	// VarsIgnorePattern is a regular expression naming variables to skip.
	VarsIgnorePattern string `json:"varsIgnorePattern"`
	// Args is `all`, `after-used`, or `none`. Default `after-used`.
	Args string `json:"args"`
	// ArgsIgnorePattern is a regular expression naming parameters to skip.
	ArgsIgnorePattern string `json:"argsIgnorePattern"`
	// CaughtErrors is `all` or `none`. Default `all`, which is oxc's answer and differs from older
	// ESLint releases where it defaulted to `none`.
	CaughtErrors string `json:"caughtErrors"`
	// CaughtErrorsIgnorePattern is a regular expression naming catch bindings to skip.
	CaughtErrorsIgnorePattern string `json:"caughtErrorsIgnorePattern"`
	// IgnoreRestSiblings keeps a binding alive when it sits beside a rest element, which is the
	// idiom for omitting a property: `const { removed, ...rest } = o`.
	IgnoreRestSiblings bool `json:"ignoreRestSiblings"`
	// DestructuredArrayIgnorePattern names array-destructured elements to skip.
	DestructuredArrayIgnorePattern string `json:"destructuredArrayIgnorePattern"`

	// The three `...IsDefault` fields record whether a pattern was configured at all, which is a
	// distinction the string alone cannot carry. An absent pattern means "leading underscore" and
	// an explicitly empty one means "ignore nothing", and collapsing them would make an empty
	// string in a config file silently turn on the underscore default.
	varsPatternIsDefault   bool
	argsPatternIsDefault   bool
	caughtPatternIsDefault bool
}

// resolveNoUnusedVarsOptions applies the defaults to whatever the config supplied.
//
// A rule configured as bare `"error"` is handed nil, and the zero value of this struct is not the
// rule's default state: it would set `args` to the empty string and both ignore patterns to
// "configured as empty", which is three wrong answers. So the nil case is written explicitly rather
// than relying on the zero value, which is the shape a shipped rule got wrong on 3,407 files.
func resolveNoUnusedVarsOptions(options any) NoUnusedVarsOptions {
	settings, _ := options.(NoUnusedVarsOptions)

	if settings.Vars == "" {
		settings.Vars = "all"
	}
	if settings.Args == "" {
		settings.Args = "after-used"
	}
	if settings.CaughtErrors == "" {
		settings.CaughtErrors = "all"
	}

	settings.varsPatternIsDefault = settings.VarsIgnorePattern == ""
	settings.argsPatternIsDefault = settings.ArgsIgnorePattern == ""
	settings.caughtPatternIsDefault = settings.CaughtErrorsIgnorePattern == ""

	return settings
}

// DecodeNoUnusedVarsOptions parses this rule's options from a config file.
//
// Exported so fixtures can route through the real decoder rather than building the struct, which is
// what puts the default resolution above under test instead of bypassing it.
func DecodeNoUnusedVarsOptions(raw json.RawMessage) (NoUnusedVarsOptions, error) {
	var decoded NoUnusedVarsOptions
	if len(raw) == 0 {
		return resolveNoUnusedVarsOptions(nil), nil
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return resolveNoUnusedVarsOptions(nil), err
	}
	return resolveNoUnusedVarsOptions(decoded), nil
}
