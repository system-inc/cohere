package tailwind

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// Whether the descriptor rows are still carrying something the ported handle bodies cannot compute.
//
// `baseDescriptors` is one of the two tables in this package that is not Tailwind's own data, and its
// stated reason is that a root's reading partitions on the value's resolved type while no
// registration shape carries both halves. That was true when it was written and the emitting half was
// not ported. It is ported now, across three slices and 242 roots, so the claim is measurable rather
// than argued and this measures it.
//
// # What a cell is, and how the branch that reaches it is built
//
// A row holds a type map, a namespace map, a fallback and an empty reading, per modifier axis. Each
// is an independent measurement, so this compares cells rather than roots, the way
// frameworkgaphandlers_test.go does.
//
// The branch per cell is read off `Lookup`'s own precedence rather than guessed, which four earlier
// attempts at this measurement got wrong in four different ways and every one of them looked like an
// emitter defect:
//
//   - A `ByType` cell is reached by an arbitrary value whose inferred type is the key, so the branch
//     is arbitrary. Sending a named value instead put `bg`, `text` and `font` on the wrong arm.
//   - A `--color` or `@colorKeyword` cell is reached through theme resolution rather than inference,
//     so `ResolvedAsColor` is what selects it, not `DataTypeColor`. Missing that reported 31 cells.
//   - A `Fallback` is a named value that matched no namespace and inferred as nothing. Whether that
//     lands on the colour arm is the row's own claim: a root whose fallback equals its `--color`
//     reading resolves an unmatched name as a colour, which is the eleven-root border family.
//   - `scale` has an empty type list, so nothing infers and `Lookup` returns `Fallback` for an
//     arbitrary value too. Its fallback is the arbitrary arm's reading rather than the named one.
//
// Each of those is a fact about how a class reaches a cell. Reconstructing them is the work this
// measurement is, and getting one wrong reads exactly like a port that cannot answer.
func TestDescriptorRowsAgreeWithTheEmitters(t *testing.T) {
	type counter struct{ compared, agreed int }
	counts := map[string]*counter{"ByType": {}, "ByNamespace": {}, "Fallback": {}, "Empty": {}}
	var disagreements []string

	record := func(kind, label string, want Reading, nodes []*Node) {
		if len(nodes) == 0 {
			return
		}
		counts[kind].compared++
		got := PropertySort(nodes)
		if fmt.Sprint(got) == fmt.Sprint(want) {
			counts[kind].agreed++
			return
		}
		disagreements = append(disagreements, fmt.Sprintf("%s %s: row %v, emitters %v", kind, label, want, got))
	}

	for root, descriptor := range baseDescriptors {
		if _, isGapRoot := gapEmitters[root]; !isGapRoot {
			continue
		}
		axis := descriptor.Absent

		for dataType, want := range axis.ByType {
			branch := UtilityBranch{HasValue: true, IsArbitrary: true, DataType: dataType}
			branch.ResolvedAsColor = dataType == DataTypeColor
			record("ByType", root+"/"+string(dataType), want, EmitGapRoot(root, branch))
		}

		for namespace, want := range axis.ByNamespace {
			branch := UtilityBranch{HasValue: true, ResolvedNamespace: namespace}
			branch.ResolvedAsColor = namespace == "--color" || namespace == namespaceColorKeyword
			if namespace == namespaceNone {
				branch.ResolvedNamespace = ""
			}
			record("ByNamespace", root+"/"+namespace, want, EmitGapRoot(root, branch))
		}

		record("Fallback", root, axis.Fallback, EmitGapRoot(root, fallbackBranchFor(descriptor)))

		if axis.Empty != nil {
			record("Empty", root, *axis.Empty, EmitGapRoot(root, UtilityBranch{}))
		}
	}

	sort.Strings(disagreements)
	var totalCompared, totalAgreed int
	for _, kind := range []string{"ByType", "ByNamespace", "Fallback", "Empty"} {
		totalCompared += counts[kind].compared
		totalAgreed += counts[kind].agreed
		t.Logf("%-12s %3d compared, %3d agreed", kind, counts[kind].compared, counts[kind].agreed)
	}
	t.Logf("descriptor cells: %d compared, %d agreed, %d disagreed", totalCompared, totalAgreed, totalCompared-totalAgreed)

	for _, disagreement := range disagreements {
		if descriptorCellsTheEmittersCannotReach[strings.SplitN(disagreement, ":", 2)[0]] {
			continue
		}
		t.Errorf("%s", disagreement)
	}

	if totalCompared == 0 {
		t.Fatal("no cell was compared, so this test measured nothing")
	}
}

// fallbackBranchFor builds the branch that reaches a row's `Fallback`.
//
// A fallback is what a named value gets when it matched no namespace and inferred as nothing. Two
// facts about the row decide which arm that is, and both are read off the row rather than assumed.
//
// A root whose fallback equals its `--color` reading resolves an unmatched name as a colour. Upstream
// that is the colour lookup running before the width lookup and returning, so the eleven border roots
// read `border-notacolor` as a colour. Eleven roots depend on this and reporting them as
// disagreements was the third wrong version of this measurement.
//
// A root with an empty type list has nothing to infer against, so `Lookup` returns `Fallback` for an
// arbitrary value as well as a named one, and the arbitrary arm is what produced the stored reading.
// `scale` and `-scale` are the two.
func fallbackBranchFor(descriptor *Descriptor) UtilityBranch {
	branch := UtilityBranch{HasValue: true}

	if colorReading, hasColor := descriptor.Absent.ByNamespace["--color"]; hasColor {
		branch.ResolvedAsColor = fmt.Sprint(colorReading) == fmt.Sprint(descriptor.Absent.Fallback)
	}
	if len(descriptor.TypeList) == 0 {
		branch.IsArbitrary = true
	}
	return branch
}

// Cells the emitters answer differently from the row, each measured rather than waved through.
//
// One entry. `text/--drop` stores `{[] 1}`: one declaration contributing no position. `--drop` is not
// a namespace `text` branches on, so the row records what the engine emitted for a value resolving
// through a namespace this root ignores, and the emitters take the ordinary named arm and produce
// `font-size` and `line-height`. That is a row describing a resolution path rather than a handle
// body's arm, which is the one thing in these rows that is not a fact about the ported bodies.
//
// An entry here is a claim that a cell is genuinely unreachable, not a way to quiet a failure. The
// test below asserts each one still disagrees, so an entry that stops being needed fails rather than
// sitting.
var descriptorCellsTheEmittersCannotReach = map[string]bool{
	"ByNamespace text/--drop": true,
}

// The exemption map holds only cells that genuinely still disagree.
func TestDescriptorExemptionsAreStillNeeded(t *testing.T) {
	if len(descriptorCellsTheEmittersCannotReach) == 0 {
		t.Fatal("the map is empty, so this test measured nothing")
	}

	for cell := range descriptorCellsTheEmittersCannotReach {
		parts := strings.Fields(cell)
		if len(parts) != 2 {
			t.Errorf("%q is not a `kind root/key` cell name", cell)
			continue
		}
		rootAndKey := strings.SplitN(parts[1], "/", 2)
		if len(rootAndKey) != 2 {
			t.Errorf("%q does not name a root and a key", cell)
			continue
		}

		descriptor, hasRow := baseDescriptors[rootAndKey[0]]
		if !hasRow {
			t.Errorf("%s exempts a root with no descriptor row", cell)
			continue
		}
		want, hasCell := descriptor.Absent.ByNamespace[rootAndKey[1]]
		if !hasCell {
			t.Errorf("%s exempts a namespace the row does not carry", cell)
			continue
		}

		// The same branch the measurement builds, not a simpler one. A first version of this guard
		// omitted `ResolvedAsColor`, so a `--color` exemption was compared on the wrong arm, always
		// disagreed, and the guard passed whatever the exemption claimed. A check that cannot fire
		// reads exactly like a check nothing violates.
		branch := UtilityBranch{HasValue: true, ResolvedNamespace: rootAndKey[1]}
		branch.ResolvedAsColor = rootAndKey[1] == "--color" || rootAndKey[1] == namespaceColorKeyword
		if rootAndKey[1] == namespaceNone {
			branch.ResolvedNamespace = ""
		}
		nodes := EmitGapRoot(rootAndKey[0], branch)
		if len(nodes) == 0 {
			t.Errorf("%s exempts a cell the emitters decline, which needs no exemption", cell)
			continue
		}
		if fmt.Sprint(PropertySort(nodes)) == fmt.Sprint(want) {
			t.Errorf("%s now agrees, so the exemption is stale and should be deleted", cell)
		}
	}
}
