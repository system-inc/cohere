package tailwind

// UtilityEvaluator evaluates `@utility` definitions against a theme.
//
// It holds the theme rather than taking it per call because the theme is what makes a repository's
// utilities repository-specific: `--value(--percentage-*)` resolves against this theme's
// `--percentage` namespace and no other, which is the whole reason the descriptor table is
// generated per repository and this evaluator is not a table at all.
type UtilityEvaluator struct {
	// Theme is the design system's resolved theme, consulted by every `--value(--namespace-*)`
	// argument.
	Theme *Theme
	// Definitions maps a utility root to its definition.
	Definitions map[string]*UtilityDefinition
}

// Has reports whether a root is defined by an `@utility` block this evaluator holds.
func (evaluator *UtilityEvaluator) Has(root string) bool {
	_, found := evaluator.Definitions[root]
	return found
}

// Reading returns the `{order, count}` reading of a functional candidate against its `@utility`
// definition, and whether the candidate compiles at all.
//
// A false return is a candidate the engine rejects, which is a null reading rather than an empty
// one. The distinction decides sort position: an empty order sorts last, and a control in Phase 0
// that read a rejection as `[]#0` reported 365,174 spurious mismatches while the model was fine.
func (evaluator *UtilityEvaluator) Reading(candidate *ParsedCandidate) (Reading, bool) {
	nodes, ok := evaluator.Compile(candidate)
	if !ok {
		return Reading{}, false
	}
	sort := PropertySort(nodes)
	return Reading(sort), true
}

// Compile evaluates a candidate against its `@utility` definition and returns the surviving body.
//
// This is `createCssUtility`'s per-candidate function. The returned nodes are the utility's body
// before it is wrapped in its selector rule and before variants are applied, which is what
// PropertySort expects and what upstream passes to `getPropertySort`.
//
// The definition is not mutated. Upstream clones the at-rule per candidate for the same reason:
// resolution rewrites declaration values in place and removes declarations, and a definition
// consumed once would answer differently on the second class that used it.
func (evaluator *UtilityEvaluator) Compile(candidate *ParsedCandidate) ([]*Node, bool) {
	return evaluator.compile(candidate, utilityRemovalsAll)
}

// compileKeepingDroppedDeclarations is Compile with the resolve-or-drop rule suppressed.
//
// Test-only, and unexported for that reason. It is the mutant TestUtilityDropIsNotDefault asserts
// the differential catches.
func (evaluator *UtilityEvaluator) compileKeepingDroppedDeclarations(candidate *ParsedCandidate) ([]*Node, bool) {
	return evaluator.compile(candidate, utilityRemovalsKeepDropped)
}

// compileWithoutRatioSplice is Compile with the ratio splice suppressed. Test-only, as above.
func (evaluator *UtilityEvaluator) compileWithoutRatioSplice(candidate *ParsedCandidate) ([]*Node, bool) {
	return evaluator.compile(candidate, utilityRemovalsKeepNonRatio)
}

func (evaluator *UtilityEvaluator) compile(candidate *ParsedCandidate, removals utilityRemovals) ([]*Node, bool) {
	if candidate == nil || candidate.Kind != ParsedCandidateKindFunctional {
		return nil, false
	}
	definition, found := evaluator.Definitions[candidate.Root]
	if !found {
		return nil, false
	}

	state := &utilityEvaluation{
		evaluator: evaluator,
		value:     candidate.Value,
		modifier:  candidate.Modifier,
	}
	body := cloneNodes(definition.Nodes)
	state.walk(body)

	// Post-condition 1. A functional utility must use `--value()` and at least one use must have
	// resolved, or the candidate is not this utility at all. `--default(…)` inside `--value(…)` is
	// how a definition opts into the omitted-value case; without it, `foo` bare does not compile.
	if !state.usedValueFunction || !state.resolvedValueFunction {
		return nil, false
	}
	// Post-condition 2. A `--modifier()` that was reached and never resolved, with a modifier
	// actually present, is a modifier the utility could not consume.
	if state.usedModifierFunction && !state.resolvedModifierFunction && candidate.Modifier != nil {
		return nil, false
	}
	// Post-condition 3. A ratio and a modifier cannot both be right: `foo-2/3` is either a fraction
	// or a value with a modifier.
	if state.resolvedRatioValue && state.resolvedModifierFunction {
		return nil, false
	}
	// Post-condition 4. The same rule read from the candidate's side: a written modifier has to be
	// consumed by something.
	if candidate.Modifier != nil && !state.resolvedRatioValue && !state.resolvedModifierFunction {
		return nil, false
	}

	// The two removals, in this order because the second depends on the whole walk having finished.
	//
	// First the drops: a declaration whose `--value()` did not resolve is not in the compiled body
	// at all. This is the mechanism the component exists for, and it is what makes arity the count
	// of survivors rather than the count of declarations written.
	//
	// Then the splice: a resolved ratio invalidates every declaration that resolved a non-ratio
	// `--value()`, because `slide-in-from-top-1/2` would otherwise also satisfy the `integer`
	// declaration through the `1` and read `[]#2`.
	var removed map[*Node]bool
	if removals != utilityRemovalsKeepDropped {
		removed = state.droppedDeclarations
	}
	if state.resolvedRatioValue && removals != utilityRemovalsKeepNonRatio {
		removed = unionNodeSets(removed, state.nonRatioDeclarations)
	}
	body = removeNodes(body, removed)

	return body, true
}
