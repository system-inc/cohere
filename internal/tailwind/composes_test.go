package tailwind

import (
	"sort"
	"strings"
	"testing"
)

// The composing answer is computed from two emissions, and this holds it against the generated table
// it replaces, class for class rather than as a rate.
//
// The table was produced by compiling two values through the real Tailwind engine and comparing
// declaration text. `ComposesFor` performs the same comparison against the ported handle bodies, so
// the two are independent measurements of one fact and are forced to agree here.
func TestComposesForAgreesWithTheGeneratedTable(t *testing.T) {
	var agreed, disagreed, unanswerable int
	var disagreements []string

	for root := range ComposingRoots {
		composes, ok := ComposesFor(root)
		if !ok {
			unanswerable++
			continue
		}
		if composes {
			agreed++
			continue
		}
		disagreed++
		disagreements = append(disagreements, root)
	}

	sort.Strings(disagreements)
	t.Logf("composing roots: %d in the table, %d answered, %d agreed, %d disagreed, %d unanswerable",
		len(ComposingRoots), agreed+disagreed, agreed, disagreed, unanswerable)

	if disagreed != 0 {
		t.Errorf("%d roots the table calls composing are computed as conflicting: %s",
			disagreed, strings.Join(disagreements, ", "))
	}
	if agreed == 0 {
		t.Fatal("no root was answered, so this test measured nothing")
	}
}

// The control, and it is the whole test.
//
// A derivation that answers "composing" for everything scores 82 of 82 against the table above and
// is worthless. This asserts the other direction on roots the table deliberately omits: `px-4` and
// `px-8` genuinely collide, and a computation that cannot say so has not learned anything.
//
// The controls are read out of the two emitter tables rather than hand-listed, so a root added
// upstream joins the population instead of being missed by a list nobody updates.
func TestComposesForRefusesRootsTheTableOmits(t *testing.T) {
	var checked, wronglyComposing int
	var wrong []string

	for root := range FrameworkMultiDeclarationUtilities {
		if ComposingRoots[root] || composingRootsTheGeneratorNeverReached[root] {
			continue
		}
		composes, ok := ComposesFor(root)
		if !ok {
			continue
		}
		checked++
		if composes {
			wronglyComposing++
			if len(wrong) < 20 {
				wrong = append(wrong, root)
			}
		}
	}
	for root := range FrameworkFunctionalUtilities {
		if ComposingRoots[root] || composingRootsTheGeneratorNeverReached[root] {
			continue
		}
		composes, ok := ComposesFor(root)
		if !ok {
			continue
		}
		checked++
		if composes {
			wronglyComposing++
			if len(wrong) < 20 {
				wrong = append(wrong, root)
			}
		}
	}

	sort.Strings(wrong)
	t.Logf("non-composing controls: %d answered, %d wrongly computed as composing", checked, wronglyComposing)

	if wronglyComposing != 0 {
		t.Errorf("%d roots the table omits are computed as composing: %s",
			wronglyComposing, strings.Join(wrong, ", "))
	}
	if checked == 0 {
		t.Fatal("no control was answered, so this test measured nothing")
	}
}

// The gap wave cannot answer composition, and this pins that rather than leaving it as a silence.
//
// `UtilityBranch` carries the value's shape and never the value, so two emissions of a gap root are
// identical whether it composes or not. Measured while writing this: 27 of 27 composing gap roots
// compare identical and so do 25 of 30 non-composing ones. `ComposesFor` therefore declines them,
// and this fails if it ever starts answering, since an answer from that path would be an artifact.
func TestComposesForDeclinesGapRoots(t *testing.T) {
	var declined, answered int
	var answeredNames []string

	for root := range gapEmitters {
		if _, ok := ComposesFor(root); ok {
			answered++
			if len(answeredNames) < 10 {
				answeredNames = append(answeredNames, root)
			}
			continue
		}
		declined++
	}

	t.Logf("gap roots: %d declined, %d answered", declined, answered)

	if answered != 0 {
		t.Errorf("%d gap roots were answered, but their emitter cannot see a value: %s",
			answered, strings.Join(answeredNames, ", "))
	}
	if declined == 0 {
		t.Fatal("no gap root was seen, so this test measured nothing")
	}
}

// Roots the computation calls composing and the generated table omits, because the generator's probe
// values never reached them.
//
// Absence from `ComposingRoots` means one of two things and they are not the same: the generator
// measured the root and found it conflicting, or the generator never measured it. The table cannot
// tell them apart, and this map is where that difference is written down.
//
// # mask-radial, measured against the live engine rather than argued
//
//	mask-radial emits utility CSS for 0 of the generator's 15 probe values
//	mask-linear emits for 15 of 15
//	mask-conic  emits for 15 of 15
//
// The probe list is the numeric and named scales. `mask-radial` takes a radial size or shape, so
// `mask-radial-4` compiles to nothing at all. The generator's loop takes the first probe that
// compiles, and on `continue`ing past all fifteen it skips the root entirely rather than recording it
// as unmeasured.
//
// Reached with values it accepts, the engine's own answer is that it composes:
//
//	mask-radial-[3px] and mask-radial-[50%] both emit
//	  mask-image: var(--tw-mask-linear), var(--tw-mask-radial), var(--tw-mask-conic)
//	  mask-composite: intersect
//
// Identical shared declarations, the value only in `--tw-mask-radial-size`, which is exactly the
// shape its two siblings have and both of them are in the table.
//
// This is the fifth instance of a failure the generator's own comment names: "A check that cannot
// fire is indistinguishable from a check nothing violates, and the same is true of a probe that
// cannot reach its subject." It lists four found before this one. The right repair is a probe list
// that reaches `mask-radial`, which belongs to whoever owns the generator; this map records the
// finding rather than silently agreeing with a hole.
var composingRootsTheGeneratorNeverReached = map[string]bool{
	"mask-radial": true,
}

// The exemption map must stay a record of measured findings rather than a place to put disagreements.
//
// Two assertions. Every entry must be a root the computation actually calls composing, so an entry
// cannot sit here excusing a root the computation agrees is conflicting. And every entry must be
// absent from `ComposingRoots`, so an entry cannot shadow a root the table already answers.
//
// A previous slice of this port shipped an exemption map that read as careful and was hiding 47
// comparisons, 7 of which disagreed. Keeping this one honest means asserting its shape rather than
// trusting its comments.
func TestTheUnreachedMapHoldsOnlyMeasuredFindings(t *testing.T) {
	if len(composingRootsTheGeneratorNeverReached) == 0 {
		t.Fatal("the map is empty, so this test measured nothing")
	}
	for root := range composingRootsTheGeneratorNeverReached {
		composes, ok := ComposesFor(root)
		if !ok {
			t.Errorf("%s is exempted but is not answerable, so the exemption hides nothing real", root)
			continue
		}
		if !composes {
			t.Errorf("%s is exempted but computes as conflicting, which the control test would pass anyway", root)
		}
		if ComposingRoots[root] {
			t.Errorf("%s is exempted but the table already carries it, so the exemption is dead", root)
		}
	}
}
