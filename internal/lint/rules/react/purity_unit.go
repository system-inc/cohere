package react

import (
	"fmt"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/ecmascript/high_level_intermediate_representation"
	utilsreact "github.com/system-inc/cohere/internal/lint/ecmascript/react"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/static_single_assignment"
)

// purityUnit is the analysis state for one compiled unit, shared by every function inside it.
type purityUnit struct {
	ctx rule.Context
	// values is the abstract state keyed by value. Single-assignment form makes one entry per
	// value rather than per binding, which is what lets a forward walk be correct without a
	// fixpoint.
	values map[static_single_assignment.IdentifierId]purityValue
	// byName is the same state keyed by source name, which is how a nested function reaches a
	// binding declared in its parent. Lowering gives such a reference a `LoadGlobal` carrying the
	// name rather than a local place, so the identifier in the child is not the identifier in the
	// parent and only the name connects them.
	byName map[string]purityValue
	// nested is what each nested function does when called, computed on first call so an uncalled
	// one costs nothing.
	nested map[*high_level_intermediate_representation.Function][]purityFinding
	// analysing guards recursion. Reachable through callbackArguments: a function passed as a
	// callback to something it calls re-enters its own frame. See findingsOf.
	analysing map[*high_level_intermediate_representation.Function]bool
	// reported is the set of nodes already reported in this unit. A wrapper called twice raises
	// the same inner finding twice and React reports it once, because the finding belongs to the
	// site that raised it rather than to each caller.
	reported map[*ast.Node]bool
}

// walk runs the forward pass over one function and returns what calling it does.
func (unit *purityUnit) walk(function *high_level_intermediate_representation.Function, emit bool) []purityFinding {
	if function == nil {
		return nil
	}

	values := unit.values
	var raised []purityFinding

	// report records a finding, and is the only place the diagnostic is built. When `emit` is
	// false the finding is still collected, because the caller needs it to decide what calling
	// this function does.
	report := func(finding purityFinding) {
		raised = append(raised, finding)
		if !emit || finding.Node == nil || unit.reported[finding.Node] {
			return
		}
		unit.reported[finding.Node] = true
		unit.ctx.ReportNode(finding.Node, rule.Message{
			Id: messagePurityImpureCallId,
			Description: fmt.Sprintf("`%s` is an impure function. %s",
				finding.CanonicalName, messagePurityImpureCallReason),
		})
	}

	for _, block := range function.Blocks {
		// A phi carries a value in from more than one path. Upstream's state merges at the join,
		// so a value that is impure on any incoming path is impure at the phi. Measured on
		// React: `let r = Math.random; if (c) { r = Date.now; } r();` reports, and the name it
		// prints is whichever path the merge kept.
		for _, phi := range block.Phis {
			// Which operand wins is invisible in a rule whose phi merge only decides a boolean, as
			// in `static_components.go`, and it is not invisible here: the operand chosen is the
			// builtin the message names. Measured on
			// `let r = Math.random; if (c) { r = Date.now; } r();`, an unordered walk produced
			// `Math.random` on some runs and `Date.now` on others, and React always says
			// `Date.now`.
			//
			// So the predecessors are visited in block order and the last impure one wins, which is
			// the operand from the latest-numbered predecessor. Blocks are in reverse postorder, so
			// that is the write that appears last in the source, which is what React prints.
			var merged purityValue
			var mergedFrom static_single_assignment.BlockId
			for _, entry := range phi.Operands {
				incoming, ok := values[entry.Place.Identifier]
				if !ok || incoming.Kind == purityValueNothing {
					continue
				}
				if merged.Kind == purityValueNothing || entry.Predecessor > mergedFrom {
					merged, mergedFrom = incoming, entry.Predecessor
				}
			}
			if merged.Kind != purityValueNothing {
				values[phi.Place.Identifier] = merged
			}
		}

		for _, instructionId := range block.Instructions {
			instruction := function.Instructions[instructionId]
			if instruction == nil {
				continue
			}
			unit.step(function, instruction, report)
		}
	}

	return raised
}

// purityStep interprets one instruction against the abstract state.
//
// Split out of the block walk so the type switch reads as the transfer function it is, one arm per
// instruction shape that moves a value.
func (unit *purityUnit) step(
	function *high_level_intermediate_representation.Function,
	instruction *high_level_intermediate_representation.Instruction,
	report func(purityFinding),
) {
	values := unit.values
	switch instructionValue := instruction.Value.(type) {
	case *high_level_intermediate_representation.LoadGlobal:
		// Only a genuine global can be one of these. An import or a module-scope binding of the same
		// name is a different thing and upstream models neither, which is what makes
		// `import Math from './m'` silent.
		if !unit.namesATrueGlobal(instruction.Node) {
			return
		}
		if _, isImpureContainer := purityImpureBuiltins[instructionValue.Name]; isImpureContainer {
			values[instruction.LValue.Identifier] = purityValue{
				Kind:          purityValueContainer,
				CanonicalName: instructionValue.Name,
			}
			return
		}
		if purityGlobalContainers[instructionValue.Name] {
			// `globalThis` itself carries no name to print; it is transparent, and the property
			// loaded off it is what names the container.
			values[instruction.LValue.Identifier] = purityValue{Kind: purityValueContainer}
			return
		}
		// Not a modelled global, so this may be a binding from an enclosing function that lowering
		// could not give a local slot. `byName` is the only link between the two, because the
		// identifier in the child function is not the identifier in the parent.
		if known, ok := unit.byName[instructionValue.Name]; ok {
			values[instruction.LValue.Identifier] = known
		}

	case *high_level_intermediate_representation.PropertyLoad:
		purityLoadProperty(function, instruction, values, instructionValue.Object, instructionValue.Property)

	case *high_level_intermediate_representation.Destructure:
		// `const {random} = Math;` reports upstream, so destructuring has to carry the signature
		// the same way a property load does. The pattern names the properties taken.
		purityDestructure(function, instruction, values, instructionValue)

	case *high_level_intermediate_representation.MethodCall:
		purityMethodCall(function, instruction, values, instructionValue, report)
		unit.callbackArguments(
			function,
			instructionValue.Arguments,
			purityCalleeName(function, instructionValue.Property),
			report,
		)

	case *high_level_intermediate_representation.CallExpression:
		unit.callExpression(function, instruction, instructionValue, report)
		unit.callbackArguments(
			function,
			instructionValue.Arguments,
			purityCalleeIdentifierName(function, instructionValue.Callee),
			report,
		)

	case *high_level_intermediate_representation.FunctionExpression:
		// Recorded but not analysed here. Analysis happens on first call, which is what keeps an
		// uncalled arrow silent.
		if int(instructionValue.Function) < len(function.Functions) {
			values[instruction.LValue.Identifier] = purityValue{
				Kind:     purityValueCallsImpure,
				Function: function.Functions[instructionValue.Function],
			}
		}

	case *high_level_intermediate_representation.StoreLocal:
		purityCopy(
			values,
			instructionValue.Value.Identifier,
			instructionValue.LValue.Identifier,
			instruction.LValue.Identifier,
		)
		unit.recordByName(function, instructionValue.LValue, instructionValue.Value)

	case *high_level_intermediate_representation.LoadLocal:
		purityCopy(values, instructionValue.Place.Identifier, instruction.LValue.Identifier)

	case *high_level_intermediate_representation.StoreContext:
		purityCopy(
			values,
			instructionValue.Value.Identifier,
			instructionValue.LValue.Identifier,
			instruction.LValue.Identifier,
		)
		unit.recordByName(function, instructionValue.LValue, instructionValue.Value)

	case *high_level_intermediate_representation.LoadContext:
		// A read of a binding captured from an enclosing function. The identifier is the enclosing
		// function's, because `Context` places are the parent's values threaded in, so the ordinary
		// copy already crosses the boundary when the parent recorded that value. The name-keyed
		// fallback covers the case where it did not, which is a binding lowering gave no local slot.
		if !purityCopy(values, instructionValue.Place.Identifier, instruction.LValue.Identifier) {
			unit.loadByName(function, instructionValue.Place, instruction)
		}

	case *high_level_intermediate_representation.StoreGlobal:
		// A reassignment through a global binding, which is how `let r = Math.random; r = Date.now`
		// lowers when the checker cannot give the binding a local identity. The value still has to
		// travel, and this is the instruction carrying it.
		purityCopy(values, instructionValue.Value.Identifier, instruction.LValue.Identifier)
	}
}

// namesATrueGlobal asks the checker whether an identifier binds to a real global rather than to
// something declared in this program.
//
// The `BindingKind` on a `LoadGlobal` cannot answer this. Lowering sets it to
// `GlobalBindingKindGlobal` unconditionally (`lower_expression.go:179`, whose own comment says
// distinguishing an import remains a later pass), so filtering on it is inert: measured, a resolved
// `import Math from './m'` arrives with kind Global and would report.
//
// So the question is asked of the checker directly. A true global is declared only in a declaration
// file, which is where the standard library lives; a module-scope binding, an import, or a
// declaration anywhere in this program has a declaration in a real source file and is not ours.
// Measured on React, `import Math from './m'` is silent, and so is a module-scope `const Math`.
//
// Every declaration is examined rather than the first. The question is "is this name declared
// anywhere in the program", so more declarations only mean more chances to disqualify it, and
// indexing `Declarations[0]` would answer about whichever one merging happened to put first.
// A name the checker cannot resolve at all is a true global: that is `Math` in a file with no
// standard library, which is what the fixture harness produces.
func (unit *purityUnit) namesATrueGlobal(node *ast.Node) bool {
	if node == nil {
		return false
	}
	identifier := node
	if !ast.IsIdentifier(identifier) {
		return false
	}
	return !rule.IsDeclaredInASourceFile(unit.ctx.TypeChecker.GetSymbolAtLocation(identifier))
}

// loadByName recovers a captured binding's state through its source name.
//
// Reached only when the identifier itself carried nothing. A nested function's `Context` place is
// minted in the child, so `const g = () => Math.random(); const f = () => g(); f();` gives the `g`
// inside `f` an identifier the parent never wrote state against, and only the name connects them.
// Measured on React, that input reports.
func (unit *purityUnit) loadByName(
	function *high_level_intermediate_representation.Function,
	place high_level_intermediate_representation.Place,
	instruction *high_level_intermediate_representation.Instruction,
) {
	if int(place.Identifier) >= len(function.Identifiers) {
		return
	}
	name := function.Identifiers[place.Identifier].Name
	if name == "" {
		return
	}
	if known, ok := unit.byName[name]; ok {
		unit.values[instruction.LValue.Identifier] = known
	}
}

// callExpression handles a call through a value rather than through a receiver.
//
// Two things can be called impurely. An `impure` value is one of the three builtins reached through
// an alias, and it reports at the call. A `callsImpure` value is a nested function, and calling it
// re-raises whatever that function does, pointing at the inner call site rather than at this one,
// which is what React does and is why `const f = () => Math.random(); f();` reports on line 2.
//
// The nested function is held as a pointer rather than as a `FunctionId`, because an id is an index
// into the declaring function's own arena and a wrapper can be reached from a sibling scope where
// that index means something else. The pointer is stable wherever the value travels.
func (unit *purityUnit) callExpression(
	function *high_level_intermediate_representation.Function,
	instruction *high_level_intermediate_representation.Instruction,
	value *high_level_intermediate_representation.CallExpression,
	report func(purityFinding),
) {
	callee, ok := unit.values[value.Callee.Identifier]
	if !ok {
		return
	}
	switch callee.Kind {
	case purityValueImpure:
		report(purityFinding{CanonicalName: callee.CanonicalName, Node: instruction.Node})
	case purityValueCallsImpure:
		for _, finding := range unit.findingsOf(callee.Function) {
			report(finding)
		}
	}
}

// callbackArguments re-raises the effects of a function passed as an argument.
//
// # Why an argument counts as a call at all
//
// A function handed to another function is usually invoked, and upstream models that: measured on
// React, every one of these reports at the inner `Math.random`, none of them naming the outer call:
//
//	props.items.map(() => Math.random())
//	doThing(() => { Math.random(); })
//	window.addEventListener('x', () => { Math.random(); })
//	props.p.then(() => { Math.random(); })
//	<li key={Math.random()} />                     (the same shape inside a map callback)
//
// Without this, the rule misses the single most common real purity bug, which is a random key on a
// mapped list. That was found by running the rule on realistic shapes rather than on the corpus,
// which contains none of them.
//
// # What is exempt, and the exemption is a hook-name test with one carve-out
//
// A hook freezes its callback, so its effects are not re-raised on the caller. `useEffect` does it
// through an explicit `Freeze { value: "@rest" }` on its signature (`globals.rs:1409`), and any
// callee whose name matches the hook pattern gets the same treatment: measured, `useCustom(() =>
// Math.random())` is silent while `doThing` with a byte-identical callback reports.
//
// `useMemo` is the one exception, and it is a semantic one rather than a special case: its callback
// is the only hook callback that runs during render, so its impurity is render impurity. Measured,
// `useMemo(() => Math.random(), [])` reports while `useCallback(() => Math.random(), [])` with the
// same callback is silent, and so are `useState`, `useRef`, `useLayoutEffect` and
// `useInsertionEffect` with a lazy initializer.
//
// So the test is: a hook-named callee exempts its callback arguments, unless it is `useMemo`.
func (unit *purityUnit) callbackArguments(
	function *high_level_intermediate_representation.Function,
	arguments []high_level_intermediate_representation.Argument,
	calleeName string,
	report func(purityFinding),
) {
	if calleeName != "useMemo" && utilsreact.IsCompilerHookName(calleeName) {
		return
	}
	for _, argument := range arguments {
		// A spread argument is not skipped explicitly, and the guard that did so was measured
		// subsumed by the kind test below: a spread's place is the iterable, so it can never hold
		// `purityValueCallsImpure`, which is only ever written against a `FunctionExpression`
		// result. A mutation neutralizing the skip changed no output.
		//
		// Recorded because upstream does reach through it and this port does not.
		// `const fns = [() => Math.random()]; doThing(...fns);` reports on React and is silent
		// here, for the same reason `const o = {r: Math.random}` is: a value stored into a
		// container is not followed back out. It is the object-property gap arriving through an
		// array rather than a new one.
		// The kind test is equivalent and kept for legibility rather than for behaviour. Only a
		// `FunctionExpression` result ever gets a non-nil `Function`, so a container or an impure
		// value reaching here hands `findingsOf` a nil and gets nil back. Measured on
		// `doThing(Math.random)`, `doThing(r)` after `const r = Math.random`, and `doThing(m)`
		// after `const m = Math`: all three silent with the test and without it.
		held, ok := unit.values[argument.Place.Identifier]
		if !ok || held.Kind != purityValueCallsImpure {
			continue
		}
		for _, finding := range unit.findingsOf(held.Function) {
			report(finding)
		}
	}
}

// findingsOf analyses a nested function on its first call and remembers the answer.
//
// # The recursion guard is reachable, and the way that was established is the point
//
// A guard was written here first. A mutation neutralizing it survived the whole fixture set, and
// probing three self-referential shapes (`const f = () => f();`, a mutually recursive pair, and a
// self-calling wrapper containing `Date.now()`) showed the in-progress branch never firing. It was
// deleted as unreachable, with that measurement recorded at the line.
//
// It was wrong, and the real tree found it: a dry run over Kirk's 3,407 files died with
// `fatal error: stack overflow`, and the cycle in the trace is
// `findingsOf -> walk -> step -> callbackArguments -> findingsOf` on one
// `*high_level_intermediate_representation.Function` pointer.
//
// Two things made the earlier verdict false. The probes predated `callbackArguments`, so the only
// path into `findingsOf` was a direct call, and a self-reference on that path resolves through
// `byName`, which the enclosing store has not written yet. Passing a function to something that
// receives it, which is what an argument is, has no such ordering, so a function reaching its own
// value as a callback argument re-enters the frame it is in.
//
// The general lesson, which is why this is written out rather than fixed quietly: an unreachability
// verdict is only as good as the paths that existed when it was measured, and adding a caller
// invalidates it silently. No fixture went red when the guard was removed, and none went red when
// the caller was added; only real code found it.
func (unit *purityUnit) findingsOf(function *high_level_intermediate_representation.Function) []purityFinding {
	if function == nil {
		return nil
	}
	if findings, analysed := unit.nested[function]; analysed {
		return findings
	}
	if unit.analysing[function] {
		return nil
	}
	unit.analysing[function] = true
	findings := unit.walk(function, false)
	delete(unit.analysing, function)
	unit.nested[function] = findings
	return findings
}

// recordByName remembers a named binding's state so a nested function can reach it.
//
// Only a place carrying a source name is recorded, because the link this restores is exactly the
// one lowering drops: a reference from a child function to a parent's binding arrives as a
// `LoadGlobal` carrying that name and nothing else.
func (unit *purityUnit) recordByName(
	function *high_level_intermediate_representation.Function,
	target high_level_intermediate_representation.Place,
	source high_level_intermediate_representation.Place,
) {
	if int(target.Identifier) >= len(function.Identifiers) {
		return
	}
	name := function.Identifiers[target.Identifier].Name
	if name == "" {
		return
	}
	if known, ok := unit.values[source.Identifier]; ok && known.Kind != purityValueNothing {
		unit.byName[name] = known
	}
}
