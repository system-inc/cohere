package core

import (
	"slices"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/ecmascript/reference"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// collectReadSymbols resolves every identifier that could be a read of a candidate, and returns the
// symbols those reads name.
//
// "Read" here is the rule's own notion and it is narrower than "appears". An identifier in a
// declaring position is not a read of itself, and a pure write is not a read: `y = 5` does not make
// `y` used, which upstream states outright in its documentation ("Write-only variables are not
// considered as used"). Both exclusions are what make the rule find anything at all.
//
// It also returns every write to those symbols, which the report needs and the read test does not:
// where the finding points, and whether the binding was ever given a value. A write here is any
// non-declaring occurrence that stores, update forms included, which is ESLint's `isWrite()`.
//
// The third map holds the symbols read through a `typeof` type query or named by a type predicate.
// typescript-eslint counts such a read as type-only, not a use, so a value whose every reference is
// one is unused and reports as "only used as a type". Measured on the installed plugin, 8.67.0:
// `const d = {}; export type D = typeof d;` reports, and so do `keyof typeof d` and
// `ReturnType<typeof f>`. A symbol here that is also read is exempt before this map is asked, and an
// import is never reported from it at all, so neither needs excluding here: upstream's exception for
// a type-only import reaches the same silence.
func collectReadSymbols(
	ruleContext rule.Context,
	sourceFile *ast.Node,
	interesting map[string]bool,
) (map[*ast.Symbol]bool, map[*ast.Symbol][]*ast.Node, map[*ast.Symbol]bool) {
	reads := map[*ast.Symbol]bool{}
	writes := map[*ast.Symbol][]*ast.Node{}
	typeOnlyReads := map[*ast.Symbol]bool{}

	var visit func(*ast.Node)
	visit = func(current *ast.Node) {
		if current == nil {
			return
		}

		if current.Kind == ast.KindIdentifier && interesting[current.Text()] &&
			current.Parent != nil && current.Parent.Kind == ast.KindTypePredicate &&
			current.Parent.AsTypePredicateNode().ParameterName == current {
			// `data is string` names the parameter without reading it. Still no read, as before, but
			// typescript-eslint words the finding "only used as a type", so it is recorded as one.
			for _, symbol := range resolveIdentifierSymbols(ruleContext, current) {
				typeOnlyReads[symbol] = true
			}
		}

		if current.Kind == ast.KindIdentifier && interesting[current.Text()] {
			isWrite := !isDeclaringName(current) && reference.WritesToBinding(current)
			for _, symbol := range resolveIdentifierSymbols(ruleContext, current) {
				if isWrite {
					writes[symbol] = append(writes[symbol], current)
				}
				if !countsAsRead(current, declaringScopeOf(ruleContext, symbol)) {
					continue
				}
				if isSelfReferenceWithinOwnDeclaration(ruleContext, current, symbol) {
					continue
				}
				if isInsideTypeQuery(current) {
					typeOnlyReads[symbol] = true
					continue
				}
				reads[symbol] = true
			}
		}

		current.ForEachChild(func(child *ast.Node) bool {
			visit(child)
			return false
		})
	}
	visit(sourceFile)

	return reads, writes, typeOnlyReads
}

// isInsideTypeQuery is typescript-eslint's `referenceContainsTypeQuery`: the identifier sits in a
// `typeof` type query, reached through nothing but qualified names, so `typeof a.b` counts for `a`.
func isInsideTypeQuery(identifier *ast.Node) bool {
	for current := identifier.Parent; current != nil; current = current.Parent {
		switch current.Kind {
		case ast.KindTypeQuery:
			return true
		case ast.KindQualifiedName:
			continue
		}
		return false
	}
	return false
}

// declaringScopeOf returns the variable scope a symbol is declared in, or nil when that cannot be
// established.
//
// The first declaration is deliberate rather than arbitrary here, and the question it answers is
// "where does this name live", which every declaration of a merged symbol answers identically: a
// symbol's declarations all bind the same name in the same scope, which is what makes them merge.
// A rule asking a different question of a merged symbol would need a different choice.
func declaringScopeOf(ruleContext rule.Context, symbol *ast.Symbol) *ast.Node {
	// This file's declarations only: the bindings this rule reports are the ones this file declares.
	declarations := rule.DeclarationsIn(ruleContext.SourceFile, symbol)
	if len(declarations) == 0 {
		return nil
	}
	return enclosingVariableScope(declarations[0])
}

// countsAsRead reports whether an identifier occurrence loads the binding's value.
//
// The two exclusions are the rule's core judgment and they run in opposite directions, which is why
// they are separated here rather than folded into one predicate.
//
// A declaring name is not a use of itself. Without this every binding would be trivially used by
// its own declaration and the rule would report nothing, the failure mode that looks exactly like
// a clean tree.
//
// A pure write is not a use. `let y = 10; y = 5;` leaves `y` unused, because nothing ever observes
// either value. An update (`y++`, `y += 1`) is deliberately not excluded: it loads before it
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
	// Only the left operand qualifies, and that is the whole discrimination: `(0, a) + 1` is a
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
	// Two neighbouring shapes are upstream pass cases and must stay clean: an immediately-invoked
	// function expression (`cb = function(a){ return cb(1+a); }()`, which runs now and so really
	// does read `cb`) and a sequence whose value is the binding itself (`cb = (function(a){...},
	// cb)`). Upstream ships both spellings of the sequence form in its fail list too, commented
	// out, at `tests/eslint.rs:743` and `:745`. It knows they should report and cannot make them.
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

	// An import specifier declares both of its names, and the export specifier below declares
	// neither of its own, which is why the two cannot share the simple `parent.Name()` test above.
	//
	// `import { a as b } from './m'` puts `a` in the `propertyName` slot and `b` in `name`. Neither
	// is a reference to anything in this file: `b` is the binding being created, and `a` names an
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
		// An export specifier inside `export { x } from './m'` names something in the other
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

	// A `typeof x` inside x's own declaration is not a use of it. `function foo(...args: typeof
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
func resolveIdentifierSymbols(ruleContext rule.Context, identifier *ast.Node) []*ast.Symbol {
	if identifier.Parent != nil && identifier.Parent.Kind == ast.KindShorthandPropertyAssignment {
		if symbol := ruleContext.TypeChecker.GetShorthandAssignmentValueSymbol(identifier.Parent); symbol != nil {
			return []*ast.Symbol{symbol}
		}
	}

	symbol := ruleContext.TypeChecker.GetSymbolAtLocation(identifier)
	if symbol == nil {
		return nil
	}

	if identifier.Parent != nil && identifier.Parent.Kind == ast.KindExportSpecifier &&
		symbol.Flags&ast.SymbolFlagsAlias != 0 {
		symbols := []*ast.Symbol{symbol}
		if aliased := resolvedAlias(ruleContext, symbol); aliased != nil && aliased != symbol {
			symbols = append(symbols, aliased)
		}
		// The local binding the specifier names, looked up in its own scope without following the alias:
		// `import { a } from 'missing'; export { a }` reads the import even when its module does not
		// resolve and resolvedAlias has nothing to give.
		if local := exportedLocalBinding(ruleContext, identifier); local != nil && !slices.Contains(symbols, local) {
			symbols = append(symbols, local)
		}
		return symbols
	}

	return []*ast.Symbol{symbol}
}

// exportedLocalBinding is the binding in this file an `export { name }` with no module specifier names,
// or nil for a re-export from another module, whose names are that module's.
func exportedLocalBinding(ruleContext rule.Context, identifier *ast.Node) *ast.Symbol {
	exportDeclaration := identifier.Parent.Parent.Parent
	if exportDeclaration == nil || exportDeclaration.Kind != ast.KindExportDeclaration ||
		exportDeclaration.AsExportDeclaration().ModuleSpecifier != nil {
		return nil
	}
	return ruleContext.TypeChecker.ResolveName(identifier.Text(), identifier,
		ast.SymbolFlagsValue|ast.SymbolFlagsType|ast.SymbolFlagsNamespace|ast.SymbolFlagsAlias, false)
}

// resolvedAlias is the declaration an import or export alias reaches, or nil when it reaches none.
//
// An alias whose module does not resolve resolves to the checker's one shared unknown symbol. Followed
// as if it were a binding, every unresolved import in a file looks like the same symbol, so one
// `export { Used }` of an unresolved import marked every other unresolved import as read, and
// `import { Unused, Used } from 'missing'; export { Used }` reported nothing (#e1zk9s0, found by
// typescript-eslint's fixer rows, which import from a module that does not exist). Asked through
// IsUnknownSymbol rather than by reading the symbol's declarations, which would be a read of an
// imported declaration and cost the rule its Shapes claim.
func resolvedAlias(ruleContext rule.Context, symbol *ast.Symbol) *ast.Symbol {
	aliased := ruleContext.TypeChecker.GetAliasedSymbol(symbol)
	if aliased == nil || ruleContext.TypeChecker.IsUnknownSymbol(aliased) {
		return nil
	}
	return aliased
}

// isReadInsideAClosureAssignedToItself reports whether a read sits inside a function expression
// that is being assigned to the very binding being read.
//
// See the call site for the two neighbouring shapes that must not match, and why.
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
			// The closure must be the direct right-hand side of the assignment. A call wrapping it
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
			// A call whose arguments contain the reference is a genuine use: the value escapes
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
			// Upstream compares the reference's variable scope against the binding's declaring
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
// element of a comma sequence whose own result is discarded. Everything else (an initializer, an
// argument, an operand, a return value, a condition) consumes it.
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
			// An assignment feeding the value straight back into the same binding discards it as
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
	unwrapped := node
	for unwrapped != nil {
		switch unwrapped.Kind {
		case ast.KindParenthesizedExpression:
			unwrapped = unwrapped.AsParenthesizedExpression().Expression
		case ast.KindNonNullExpression:
			unwrapped = unwrapped.AsNonNullExpression().Expression
		case ast.KindAsExpression:
			unwrapped = unwrapped.AsAsExpression().Expression
		default:
			return unwrapped
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
