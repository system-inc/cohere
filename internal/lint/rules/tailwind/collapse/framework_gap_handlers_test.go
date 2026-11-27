package tailwind

import (
	"sort"
	"strings"
	"testing"
)

// gapRoots is the 55 roots this slice ports, named rather than derived from the emitter map.
//
// Written out so that a root silently dropped from `gapEmitters` fails rather than shrinking the
// population this test measures. That failure mode is the one this package keeps finding: a
// measurement over the wrong population reads exactly like a passing one.
var gapRoots = []string{
	"backdrop-filter", "bg", "bg-conic", "bg-radial", "border", "border-b", "border-be",
	"border-bs", "border-e", "border-l", "border-r", "border-s", "border-t", "border-x", "border-y",
	"decoration", "drop-shadow", "duration", "fill", "filter", "flex", "font",
	"inset-ring", "inset-shadow", "mask", "outline", "ring", "rotate", "scale", "shadow", "stroke",
	"text", "text-shadow", "transform",
	"mask-b-from", "mask-b-to", "mask-conic-from", "mask-conic-to", "mask-l-from", "mask-l-to",
	"mask-linear-from", "mask-linear-to", "mask-r-from", "mask-r-to", "mask-radial-from",
	"mask-radial-to", "mask-t-from", "mask-t-to", "mask-x-from", "mask-x-to", "mask-y-from",
	"mask-y-to",
	"-bg-conic", "-rotate", "-scale",
}

// gapProbe is one measured cell of a root's descriptor and the branch that should reproduce it.
//
// A descriptor row is three parallel axes, each holding a type map, a namespace map, a fallback and
// an empty reading. Every populated cell is an independent measurement against Tailwind, so each
// one is a separate comparison here rather than one comparison per root.
type gapProbe struct {
	// name is what the cell is, for a failure message: `Absent.ByType[length]`.
	name string
	// branch is the input that should make the emitter produce the cell's reading.
	branch UtilityBranch
	// reading is the measured side.
	reading Reading
}

// gapProbesFor turns one root's descriptor row into the cells this test compares.
//
// The mapping from a cell to a branch is the claim under test, and it is the same one the package
// comment on frameworkgaphandlers.go states: a `ByType` cell is an arbitrary value of that type, a
// `--color` or `@colorKeyword` cell is a named value the theme answered as a colour, an `@none`
// cell is a named value it did not, and `Empty` is a class written with no value.
//
// A cell whose branch cannot be expressed is skipped by name rather than silently, and
// TestGapRootExemptionsAreExactlyTheStaticCollision asserts the skipped set is the one this file
// documents.
func gapProbesFor(descriptor *Descriptor) []gapProbe {
	var probes []gapProbe
	axes := []struct {
		name        string
		axis        AxisReadings
		hasModifier bool
	}{
		{"Absent", descriptor.Absent, false},
		{"Alpha", descriptor.Alpha, true},
	}
	for _, axis := range axes {
		for dataType, reading := range axis.axis.ByType {
			probes = append(probes, gapProbe{
				name: axis.name + ".ByType[" + string(dataType) + "]",
				branch: UtilityBranch{
					HasValue:    true,
					IsArbitrary: true,
					DataType:    dataType,
					HasModifier: axis.hasModifier,
				},
				reading: reading,
			})
		}
		for namespace, reading := range axis.axis.ByNamespace {
			switch namespace {
			case "--color", namespaceColorKeyword:
				probes = append(probes, gapProbe{
					name:    axis.name + ".ByNamespace[" + namespace + "]",
					branch:  UtilityBranch{HasValue: true, ResolvedAsColor: true, HasModifier: axis.hasModifier},
					reading: reading,
				})
			case namespaceNone:
				probes = append(probes, gapProbe{
					name:    axis.name + ".ByNamespace[@none]",
					branch:  UtilityBranch{HasValue: true, HasModifier: axis.hasModifier},
					reading: reading,
				})
			}
			// Every other namespace is a repository's own token bucket, such as `--shadow` or
			// `--text`. Those are compared by TestGapRootThemeNamespacesAreNamed rather than here,
			// because which namespace a root consumes is per-root data this file would have to
			// restate to probe.
		}
		if axis.axis.Empty != nil {
			probes = append(probes, gapProbe{
				name:    axis.name + ".Empty",
				branch:  UtilityBranch{HasModifier: axis.hasModifier},
				reading: *axis.axis.Empty,
			})
		}
		// The fallback, probed as an arbitrary value that infers as nothing.
		//
		// This cell is where an arbitrary arm's reading lands for a root whose row has no ByType
		// entry, and leaving it out left a real hole. Deleting `scale`'s entire arbitrary branch
		// changed no cell this test compared, because `scale` has no ByType map at all and its
		// one-declaration arbitrary reading lives here. The mutation was caught only by
		// TestGapRootArbitraryAndNamedCanDiffer, which probes that one root by hand.
		//
		// Skipped when the axis has no fallback, which is a zero Reading rather than an absent one
		// and would otherwise be compared as a real measurement of nothing.
		if axis.axis.Fallback.Count != 0 {
			probes = append(probes, gapProbe{
				name:    axis.name + ".Fallback",
				branch:  UtilityBranch{HasValue: true, IsArbitrary: true, HasModifier: axis.hasModifier},
				reading: axis.axis.Fallback,
			})
		}
	}
	sort.Slice(probes, func(first, second int) bool { return probes[first].name < probes[second].name })
	return probes
}

// gapCellsMeasuredElsewhere is every cell this test deliberately does not compare, with its reason.
//
// Keyed on the exact cell rather than on a root, and that precision is the point. A first version of
// this map exempted whole roots, which read as eight documented exceptions and was in fact hiding 47
// cells. Measured by disabling the skips: of those 47, exactly 7 disagreed and 40 were passing
// comparisons being thrown away under a comment explaining why they could not be made. The reasons
// written for `shadow`, `inset-shadow`, `drop-shadow`, `text-shadow`, `text` and `font` were all
// true of some cell of those roots and true of none of the cells that were being skipped.
//
// So the one that remains is the whole exemption.
//
// `flex` is a `staticUtility` declaring `display` at utilities.ts:973 and a functional root beside
// it. The extractor probed the bare class as the functional root, so the row's `Empty` records the
// static utility's reading, while the functional body refuses an empty value and answers every
// value form, which is compared like any other root's.
//
// `block` and `inline` share a name with a `display` static the same way and have a measured row,
// but they are not gap roots: utilities.ts:1147 and :1150 register them through `spacingUtility`
// beside `min-inline` and `max-block`, so they are in the multi-declaration slice, and
// TestBlockAndInlineAreDisplayStaticsAndSizingRoots compares their rows against it.
//
// TestGapRootExemptionsAreExactlyTheStaticCollision fails if this map grows.
var gapCellsMeasuredElsewhere = map[string]string{
	"flex.Absent.Empty": "the functional root refuses an empty value; the row records the static display utility",
}

// TestGapRootEmittersAgreeWithTheirMeasuredReadings is the acceptance test for this slice.
//
// Every one of the 55 roots has a `baseDescriptors` row carrying a measured `{order, count}` per
// value type, produced by asking Tailwind rather than by reading the source. Every root now also
// has an emitter transcribed from `utilities.ts`. The two were produced by different means from
// different sources, so forcing them to agree cell for cell is a real comparison.
//
// The comparison is on the reading, because that is what every consumer asks for and because
// `PropertySort` is where a wrong property name stops being a typo and becomes a wrong sort key.
// TestGapRootPropertyNamesAgreeWithTheTable compares the names, which the reading cannot separate
// for two properties `PropertyOrder` does not know.
func TestGapRootEmittersAgreeWithTheirMeasuredReadings(test *testing.T) {
	test.Parallel()
	var agreed int
	var compared int
	var disagreements []string
	var unported []string

	// Which skip keys actually matched. A key that matches nothing is not a harmless typo: it reads
	// as a documented exemption while the cell it names is being compared anyway, or worse, while a
	// different cell goes uncompared. The first run of this test had exactly that, `flex.Empty`
	// against a probe named `Absent.Empty`, and it surfaced as a real disagreement only by luck.
	used := map[string]bool{}

	for _, root := range gapRoots {
		descriptor, found := baseDescriptors[root]
		if !found {
			test.Errorf(
				"%s is in the list this slice ports and has no baseDescriptors row, so there is nothing to compare it against",
				root,
			)
			continue
		}
		if _, ported := gapEmitters[root]; !ported {
			unported = append(unported, root)
			continue
		}
		for _, probe := range gapProbesFor(descriptor) {
			if _, skipped := gapCellsMeasuredElsewhere[root+"."+probe.name]; skipped {
				used[root+"."+probe.name] = true
				continue
			}
			emitted := EmitGapRoot(root, probe.branch)
			sorted := PropertySort(emitted)
			computed := Reading(sorted)

			compared++
			if computed.Equal(probe.reading) {
				agreed++
				continue
			}
			disagreements = append(disagreements,
				root+" "+probe.name+": Tailwind measured "+formatReading(probe.reading)+
					", the ported handle emits "+describeDeclarations(emitted)+" reading "+formatReading(computed))
		}
	}

	sort.Strings(disagreements)
	sort.Strings(unported)

	// The population, stated before the disagreements. A run that compared nothing disagrees about
	// nothing and is otherwise indistinguishable from a run that compared everything.
	test.Logf("gap handles: %d roots in this slice, %d cells compared, %d agreed, %d unported, %d disagreed",
		len(gapRoots), compared, agreed, len(unported), len(disagreements))

	if len(gapRoots) != 55 {
		test.Errorf(
			"this slice lists %d roots and the assertions below were sized for 55; the population moved",
			len(gapRoots),
		)
	}
	if compared == 0 {
		test.Error("no cell was compared, so this test proves nothing about any emitter")
	}
	if agreed != compared {
		test.Errorf("%d of %d cells agreed; the acceptance condition is every cell, not a rate", agreed, compared)
	}
	for _, root := range unported {
		test.Errorf("%s has a baseDescriptors row and no ported handle, so it emits nothing and reads nothing", root)
	}
	for _, disagreement := range disagreements {
		test.Error(disagreement)
	}

	// Every documented exemption has to have fired. See `used`.
	var unmatched []string
	for key := range gapCellsMeasuredElsewhere {
		if !used[key] {
			unmatched = append(unmatched, key)
		}
	}
	sort.Strings(unmatched)
	for _, key := range unmatched {
		test.Errorf("gapCellsMeasuredElsewhere names %q and no cell matched it; an exemption that never fires "+
			"documents a skip that is not happening, which is how a cell goes uncompared under a comment "+
			"saying why it was skipped", key)
	}
}

// TestEveryGapRootHasAnEmitter pins that the port covers the whole slice.
//
// Separate from the acceptance test because they fail differently: this one catches a root nobody
// wrote a body for, where the acceptance test above would report it as unported and could be read as
// a smaller population rather than as a gap.
func TestEveryGapRootHasAnEmitter(test *testing.T) {
	test.Parallel()
	var missing []string
	for _, root := range gapRoots {
		if _, ported := gapEmitters[root]; !ported {
			missing = append(missing, root)
		}
	}
	sort.Strings(missing)
	for _, root := range missing {
		test.Errorf("%s is one of the 55 and has no entry in gapEmitters", root)
	}

	// And the reverse. An emitter for a root outside this slice would be a body nothing compares
	// against, which is the state the port exists to remove.
	inSlice := make(map[string]bool, len(gapRoots))
	for _, root := range gapRoots {
		inSlice[root] = true
	}
	for root := range gapEmitters {
		if !inSlice[root] {
			test.Errorf(
				"gapEmitters carries %s, which is not one of the 55 this slice ports and which no test compares",
				root,
			)
		}
	}
	test.Logf("emitters: %d roots in the slice, %d in gapEmitters", len(gapRoots), len(gapEmitters))
}

// TestGapRootPropertyNamesAreDistinctPerArm replaces a comparison whose other side is gone.
//
// This compared the emitters against `RootDeclaredProperties` and expected to disagree: the table
// stored one property list per root while these roots declare different properties per value, and 31
// of 57 differed. #mz0m6k8 deleted that table precisely because the emitter is the correct side, so
// the comparison has nothing left to compare against.
//
// What it was really asserting was that the arms are distinct, and that is asserted directly here.
// A root whose arms returned the same property list would have made the old comparison report agreement, which
// is the direction that reads as success, so this is the check that carried the weight.
func TestGapRootPropertyNamesAreDistinctPerArm(test *testing.T) {
	test.Parallel()
	// The roots that branch colour against not-colour. Each must declare different properties on the
	// two arms, or a width and a colour of the same root would read as conflicting.
	var distinct int
	var identical int
	for _, root := range gapRoots {
		ordinary := PropertySort(EmitGapRoot(root, UtilityBranch{HasValue: true}))
		colour := PropertySort(EmitGapRoot(root, UtilityBranch{HasValue: true, ResolvedAsColor: true}))
		if len(ordinary.Order) == 0 && len(colour.Order) == 0 {
			continue
		}
		if readingsEqual(
			Reading(ordinary),
			Reading(colour),
		) {
			identical++
			continue
		}
		distinct++
	}

	test.Logf("arms: %d gap roots read differently for a colour than for their ordinary value, %d read the same",
		distinct, identical)

	// Named rather than asserted loosely. The border family alone is eleven roots that must split,
	// and a zero here means every root now returns one answer for both values, which is the flattening
	// the deleted override tables existed to work around.
	if distinct == 0 {
		test.Error("no gap root distinguishes a colour value from its ordinary one, so every branch has " +
			"been flattened and a width would conflict with a colour on the same root")
	}
}

// TestObjectRootAndItsStaticsDeclareDifferentProperties is what the table defect looked like once
// the table was gone.
//
// `RootDeclaredProperties` stored `[object-fit]` for root `object`, and the functional registration
// declares `object-position`: `object-cover` is a static declaring a fit and `object-center` is the
// functional root declaring a position. One row could hold only one of those, so
// `ClassDeclaredProperties` patched nine `object-*` classes back while `object-cover` and
// `object-fill` were correct only because they happened to read the row.
//
// Both tables are deleted. This asserts the two registrations still answer separately, which is what
// makes the patch unnecessary rather than merely absent.
func TestObjectRootAndItsStaticsDeclareDifferentProperties(test *testing.T) {
	test.Parallel()
	utility, known := FrameworkFunctionalUtilities["object"]
	if !known {
		test.Fatal("object is not in FrameworkFunctionalUtilities")
	}
	emitted := utility.Emit("object", ResolvedUtilityValue{Value: "zzsentinel"})
	if len(emitted) != 1 || emitted[0].Property != "object-position" {
		test.Errorf("the functional root object emits %s; it declares object-position, and reading "+
			"object-fit here is the defect the deleted table had", describeDeclarations(emitted))
	}

	for _, className := range []string{"object-cover", "object-fill", "object-contain"} {
		declarations, isStatic := FrameworkStaticDeclarations[className]
		if !isStatic {
			test.Errorf(
				"%s is not in FrameworkStaticDeclarations; it is a staticUtility declaring object-fit",
				className,
			)
			continue
		}
		if len(declarations) != 1 || declarations[0].Property != "object-fit" {
			test.Errorf("%s declares %v; upstream registers it as a static object-fit", className, declarations)
		}
	}
}

// TestBlockAndInlineAreDisplayStaticsAndSizingRoots pins both registrations each name carries.
//
// utilities.ts registers `block` and `inline` twice at the v4.3.3 tag: as a `staticUtility`
// declaring `display` (lines 956 and 958) and as a `spacingUtility` declaring `block-size` and
// `inline-size` (lines 1147 and 1150), in the same loop as `min-inline` and `max-block`. The port
// once read only the first, so `inline-72` was reported unknown and every class list holding one was
// misordered.
//
// The measured `baseDescriptors` row is the independent side. The extractor asked Tailwind for every
// value form of each root, so the row's value cells are the functional root's reading and its
// `Empty` is the static's, and the ported table and emitter are held to both.
func TestBlockAndInlineAreDisplayStaticsAndSizingRoots(test *testing.T) {
	test.Parallel()
	for _, registration := range []struct {
		root      string
		property  string
		themeKeys []string
	}{
		{root: "block", property: "block-size", themeKeys: []string{"--spacing"}},
		{root: "inline", property: "inline-size", themeKeys: []string{"--spacing", "--container"}},
	} {
		// The static form still reads display, which is what `flex block` conflicting rests on.
		static, isStatic := FrameworkStaticDeclarations[registration.root]
		if !isStatic || len(static) != 1 || static[0].Property != "display" {
			test.Errorf("the static %s declares %v; utilities.ts registers it as a static display", registration.root, static)
		}

		utility, known := FrameworkMultiDeclarationUtilities[registration.root]
		if !known {
			test.Errorf("%s has no functional registration in the port; utilities.ts registers it through "+
				"spacingUtility declaring %s", registration.root, registration.property)
			continue
		}
		// `spacingUtility`'s own options: the root's namespaces, fractions, the `--spacing`
		// multiplier for a bare number, no negative, and `defaultValue: null`, which refuses the bare
		// class and leaves it to the static.
		if strings.Join(utility.ThemeKeys, " ") != strings.Join(registration.themeKeys, " ") ||
			!utility.SupportsFractions || utility.BareValue != BareValueSpacingMultiplier ||
			utility.SupportsNegative || !utility.RefusesEmptyValue {
			test.Errorf("%s is described as %+v; upstream's spacingUtility passes themeKeys %v, supportsFractions, "+
				"the spacing multiplier, no negative and a null default", registration.root, utility,
				registration.themeKeys)
		}

		emitted := utility.Emit(registration.root, "", ResolvedUtilityValue{Value: "zzsentinel"})
		if len(emitted) != 1 || emitted[0].Property != registration.property {
			test.Errorf("%s-72 emits %s; upstream's handle declares %s alone", registration.root,
				describeDeclarations(emitted), registration.property)
			continue
		}
		reading := Reading(PropertySort(emitted))

		descriptor, found := baseDescriptors[registration.root]
		if !found {
			test.Errorf("%s has no baseDescriptors row, so there is no measurement to hold the port to", registration.root)
			continue
		}
		var valueCells int
		for _, axis := range []struct {
			name     string
			readings AxisReadings
		}{
			{"Absent", descriptor.Absent},
			{"Alpha", descriptor.Alpha},
		} {
			cells := map[string]Reading{}
			for namespace, measured := range axis.readings.ByNamespace {
				cells["ByNamespace["+namespace+"]"] = measured
			}
			for dataType, measured := range axis.readings.ByType {
				cells["ByType["+string(dataType)+"]"] = measured
			}
			if axis.readings.Fallback.Count != 0 {
				cells["Fallback"] = axis.readings.Fallback
			}
			for cell, measured := range cells {
				valueCells++
				if !readingsEqual(measured, reading) {
					test.Errorf("%s %s.%s: Tailwind measured %s, the ported %s reads %s", registration.root,
						axis.name, cell, formatReading(measured), registration.property, formatReading(reading))
				}
			}
		}
		if valueCells == 0 {
			test.Errorf("%s's row holds no value cell, so the functional reading was compared against nothing",
				registration.root)
		}

		// The row's empty form is the static's reading, since the functional root refuses it.
		if descriptor.Absent.Empty == nil {
			test.Errorf("%s has no Empty reading; the static display utility answers the bare class", registration.root)
			continue
		}
		empty := *descriptor.Absent.Empty
		if position := PropertyOrder["display"]; len(empty.Order) != 1 || empty.Order[0] != position {
			test.Errorf("%s reads %s for its empty form; the bare class is the static display utility",
				registration.root, formatReading(empty))
		}
	}
}

// TestShadowFamilyAlphaDeclarationDecidesTheCount pins the branch the milestone did not name.
//
// The shadow family writes `decl('<prefix>-alpha', alpha)` where `alpha` is undefined without a
// modifier. `PropertySort` skips a declaration whose value is absent and counts one whose value is
// empty, so that single declaration is the entire difference between a root's unmodified `#2` and
// its modified `#3`.
//
// That is a branch on the candidate rather than on the value, which is why the emitters here take a
// `UtilityBranch` and not a `ResolvedUtilityValue`: resolution consumes the modifier and reports
// nothing about it, so on a resolved value alone this branch is unreachable.
func TestShadowFamilyAlphaDeclarationDecidesTheCount(test *testing.T) {
	test.Parallel()
	for _, root := range []string{"shadow", "inset-shadow", "drop-shadow", "text-shadow"} {
		unmodified := PropertySort(EmitGapRoot(root, UtilityBranch{HasValue: true}))
		modified := PropertySort(EmitGapRoot(root, UtilityBranch{HasValue: true, HasModifier: true}))

		if modified.Count != unmodified.Count+1 {
			test.Errorf("%s counts %d unmodified and %d modified; the alpha declaration is absent-valued without a "+
				"modifier and present with one, so the modified form must count exactly one more",
				root, unmodified.Count, modified.Count)
		}
		if len(modified.Order) != len(unmodified.Order) {
			test.Errorf(
				"%s reads %d positions unmodified and %d modified; the alpha property is outside PropertyOrder, "+
					"so it changes the count and never the order",
				root,
				len(unmodified.Order),
				len(modified.Order),
			)
		}
	}
}

// TestGapRootArbitraryAndNamedCanDiffer pins the branch no data type can express.
//
// `scale-[2]` emits one declaration and `scale-150` emits four. Both infer nothing useful and both
// are the same root, so the only thing separating them is that upstream's arbitrary arm returns
// before reaching the axis variables.
//
// Without this the `IsArbitrary` field could be deleted and every type-keyed comparison above would
// still pass, since the type maps are probed with arbitrary branches throughout.
func TestGapRootArbitraryAndNamedCanDiffer(test *testing.T) {
	test.Parallel()
	arbitrary := PropertySort(EmitGapRoot("scale", UtilityBranch{HasValue: true, IsArbitrary: true}))
	named := PropertySort(EmitGapRoot("scale", UtilityBranch{HasValue: true}))

	if arbitrary.Count != 1 {
		test.Errorf(
			"scale-[2] emitted %d declarations; upstream's arbitrary arm returns a single scale declaration",
			arbitrary.Count,
		)
	}
	if named.Count != 4 {
		test.Errorf(
			"scale-150 emitted %d declarations; upstream's named path writes three axis variables and the shorthand",
			named.Count,
		)
	}
	if arbitrary.Count == named.Count {
		test.Error("scale reads the same for an arbitrary and a named value, so IsArbitrary is deletable and this " +
			"slice's branch model is carrying a field nothing turns on")
	}
}

// TestGapRootExemptionsAreExactlyTheStaticCollision pins the size and the shape of the hole.
//
// Every exemption in gapCellsMeasuredElsewhere has to be a cell of `flex`, and there has to be one of
// them. Both halves matter and they fail differently.
//
// The count catches the map growing. An exemption is the one mechanism in this file that turns a
// disagreement into a silence, so a second entry appearing is a cell that stopped being compared,
// and it should cost somebody a line here rather than passing quietly.
//
// The shape catches the map broadening. An earlier version keyed on roots instead of cells, which
// exempted 47 cells to excuse 7; the other 40 were agreeing all along and were being discarded
// under a comment saying they could not be compared. Requiring every key to name one cell of the
// collided root makes that mistake unrepresentable rather than merely discouraged.
func TestGapRootExemptionsAreExactlyTheStaticCollision(test *testing.T) {
	test.Parallel()
	if len(gapCellsMeasuredElsewhere) != 1 {
		test.Errorf("gapCellsMeasuredElsewhere holds %d exemptions and this test was written against 1; "+
			"an exemption is the one way a cell stops being compared without failing, so growing the map "+
			"should cost a line here", len(gapCellsMeasuredElsewhere))
	}
	for key := range gapCellsMeasuredElsewhere {
		root, _, split := strings.Cut(key, ".")
		if !split {
			test.Errorf("%q exempts a whole root rather than a cell; that is the shape that hid 41 passing "+
				"comparisons behind 6 real ones", key)
			continue
		}
		if root != "flex" {
			test.Errorf("%q exempts a cell of %s; the only gap row this port cannot answer is flex's, whose "+
				"staticUtility declaring display stands where the extractor probed for a functional one", key, root)
		}
	}
}
