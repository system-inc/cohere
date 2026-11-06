package high_level_intermediate_representation

import (
	shimchecker "github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/system-inc/cohere/static_single_assignment"
)

type scopeMerger struct {
	parentScope         *ScopeId
	nestedScopesRemoved int
	function            *Function
	dependencies        *ScopeDependencies
	typeChecker         *shimchecker.Checker
	usage               *LastUsage
	// temporaries resolves a StoreLocal chain, so a value copied into a temporary is still
	// recognised as the earlier scope's output. See `declaredByOrAliasedFrom`.
	temporaries map[static_single_assignment.DeclarationId]static_single_assignment.DeclarationId
	merges      int
	// declarationsPruned counts declarations dropped because the widened range left them dead.
	declarationsPruned int
	// declarationPruneCalls counts how many times the pruning ran, which is the control on the
	// zero above.
	declarationPruneCalls int
}

func (merger *scopeMerger) mergeBlock(block ReactiveBlock) ReactiveBlock {
	// Nested blocks first, so an inner merge is settled before the outer scan reads the statement
	// holding it.
	rebuilt := make(ReactiveBlock, 0, len(block))
	for _, statement := range block {
		switch shape := statement.(type) {
		case *ReactiveScopeBlock:
			parent := merger.parentScope
			if !shape.Pruned {
				scope := shape.Scope
				merger.parentScope = &scope
			}
			shape.Instructions = merger.mergeBlock(shape.Instructions)
			merger.parentScope = parent
			if !shape.Pruned && parent != nil && AreEqualDependencies(
				merger.dependencies.DependenciesOf(*parent),
				merger.dependencies.DependenciesOf(shape.Scope),
			) {
				merger.nestedScopesRemoved++
				rebuilt = append(rebuilt, shape.Instructions...)
				continue
			}
		case *ReactiveTerminalStatement:
			merger.mergeTerminalBlocks(shape)
		}
		rebuilt = append(rebuilt, statement)
	}

	var candidate *mergeCandidate
	var committed []mergeCandidate

	// commit records a candidate if it spans more than one scope, then clears it.
	commit := func() {
		if candidate != nil && candidate.to > candidate.from+1 {
			committed = append(committed, *candidate)
		}
		candidate = nil
	}

	for index, statement := range rebuilt {
		switch shape := statement.(type) {
		case *ReactiveTerminalStatement:
			// Upstream does not merge across terminals.
			commit()

		case *ReactiveInstructionStatement:
			if shape.Instruction == nil {
				commit()
				continue
			}
			plain, isPlain := shape.Instruction.Value.(*ReactiveInstructionValue)
			if !isPlain || plain.Value == nil {
				commit()
				continue
			}
			if store, isStore := plain.Value.(*StoreLocal); isStore {
				if candidate == nil {
					continue
				}
				// Upstream allows a StoreLocal only while tracking it, recording the alias so a
				// later dependency on the stored name still resolves to the earlier scope's output.
				target := declarationOf(merger.function, store.LValue.Identifier)
				source := declarationOf(merger.function, store.Value.Identifier)
				if aliased, found := merger.temporaries[source]; found {
					source = aliased
				}
				merger.temporaries[target] = source
				candidate.lvalues = append(candidate.lvalues, target)
				continue
			}
			if !mergeAllowedInstruction(plain.Value) {
				commit()
				continue
			}
			if candidate != nil && shape.Instruction.LValue != nil {
				candidate.lvalues = append(candidate.lvalues,
					declarationOf(merger.function, shape.Instruction.LValue.Identifier))
			}

		case *ReactiveScopeBlock:
			if shape.Pruned {
				// Upstream does not merge across pruned scopes.
				commit()
				continue
			}
			if candidate != nil &&
				CanMergeScopes(merger.function, candidate.scope.Scope, shape.Scope, merger.dependencies,
					merger.temporaries, merger.typeChecker) &&
				AreLValuesLastUsedByScope(shape.Range.End, candidate.lvalues, merger.usage) {
				// Widen the survivor to cover the scope it absorbs, matching upstream's range
				// update, and reopen the lvalue set: everything named before this point is now
				// inside the merged scope.
				if shape.Range.End > candidate.scope.Range.End {
					candidate.scope.Range.End = shape.Range.End
				}
				candidate.to = index + 1
				candidate.lvalues = candidate.lvalues[:0]
				// Upstream's `updateScopeDeclarations`, called at exactly this point: the survivor's
				// range has just widened, so a value it declares but which is last read inside the
				// absorbed range is no longer an output of the merged scope.
				//
				// This was left unported when the merge landed, on the reasoning that a wider
				// declaration set is conservative. That holds only where declarations are read to
				// permit; `pruneAlwaysInvalidatingScopes` reads them to propagate, which prunes
				// further scopes downstream, so the debt came due when that pass was written.
				merger.declarationPruneCalls++
				merger.declarationsPruned += merger.dependencies.PruneDeclarationsLastUsedBefore(
					candidate.scope.Scope, candidate.scope.Range.End, merger.usage, merger.function)
				if !ScopeIsEligibleForMerging(merger.function, shape.Scope, merger.dependencies,
					merger.typeChecker) {
					commit()
				}
				continue
			}
			commit()
			if ScopeIsEligibleForMerging(merger.function, shape.Scope, merger.dependencies, merger.typeChecker) {
				candidate = &mergeCandidate{scope: shape, from: index, to: index + 1}
			}
		}
	}
	commit()

	if len(committed) == 0 {
		return rebuilt
	}
	return merger.rewrite(rebuilt, committed)
}

// rewrite splices each committed candidate's statements into its surviving scope.
func (merger *scopeMerger) rewrite(block ReactiveBlock, committed []mergeCandidate) ReactiveBlock {
	rebuilt := make(ReactiveBlock, 0, len(block))
	index := 0
	for _, entry := range committed {
		if index < entry.from {
			rebuilt = append(rebuilt, block[index:entry.from]...)
			index = entry.from
		}
		survivor, isScope := block[entry.from].(*ReactiveScopeBlock)
		if !isScope {
			// Upstream raises an invariant here. A linter cannot, so the run is abandoned and the
			// statements are copied through unchanged rather than being silently dropped.
			continue
		}
		rebuilt = append(rebuilt, survivor)
		index++
		for index < entry.to {
			switch shape := block[index].(type) {
			case *ReactiveScopeBlock:
				survivor.Instructions = append(survivor.Instructions, shape.Instructions...)
				survivor.Merged = append(survivor.Merged, shape.Scope)
				merger.merges++
			default:
				// Never taken on the corpus: 132 scopes fold and zero non-scope statements come
				// with them, so a mutation deleting this line changes no answer. It is kept because
				// the allowlist explicitly permits instructions between two merging scopes, and a
				// pass that silently dropped them the day one appeared would lose real work. The
				// zero is a fact about this corpus, not about the shape.
				survivor.Instructions = append(survivor.Instructions, block[index])
			}
			index++
		}
	}
	rebuilt = append(rebuilt, block[index:]...)
	return rebuilt
}

// mergeTerminalBlocks descends into every block a terminal contains.
//
// Each arm assigns the result back, for the reason `transformTerminalBlocks` records: a missing
// assignment leaves the terminal holding the pre-merge slice, and that is invisible to any test
// counting what the walk reached.
func (merger *scopeMerger) mergeTerminalBlocks(statement *ReactiveTerminalStatement) {
	switch shape := statement.Terminal.(type) {
	case *ReactiveIf:
		shape.Consequent = merger.mergeBlock(shape.Consequent)
		if shape.Alternate != nil {
			rewritten := merger.mergeBlock(*shape.Alternate)
			shape.Alternate = &rewritten
		}
	case *ReactiveSwitch:
		for index := range shape.Cases {
			if shape.Cases[index].Block != nil {
				rewritten := merger.mergeBlock(*shape.Cases[index].Block)
				shape.Cases[index].Block = &rewritten
			}
		}
	case *ReactiveFor:
		shape.Loop = merger.mergeBlock(shape.Loop)
	case *ReactiveForOf:
		shape.Loop = merger.mergeBlock(shape.Loop)
	case *ReactiveForIn:
		shape.Loop = merger.mergeBlock(shape.Loop)
	case *ReactiveWhile:
		shape.Loop = merger.mergeBlock(shape.Loop)
	case *ReactiveDoWhile:
		shape.Loop = merger.mergeBlock(shape.Loop)
	case *ReactiveLabelTerminal:
		shape.Block = merger.mergeBlock(shape.Block)
	case *ReactiveTry:
		shape.Block = merger.mergeBlock(shape.Block)
		shape.Handler = merger.mergeBlock(shape.Handler)
	}
}
