package high_level_intermediate_representation

import (
	"strings"
	"testing"
)

func TestPhiValueKindsPreserveMixedFrozenValues(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name        string
		left, right EffectValueKind
		want        EffectValueKind
		unseen      bool
	}{
		{"frozen mutable", EffectValueFrozen, EffectValueMutable, EffectValueMaybeFrozen, false},
		{"mutable frozen", EffectValueMutable, EffectValueFrozen, EffectValueMaybeFrozen, false},
		{"mixed primitive", EffectValueMaybeFrozen, EffectValuePrimitive, EffectValueMaybeFrozen, false},
		{"mixed global", EffectValueMaybeFrozen, EffectValueGlobal, EffectValueMaybeFrozen, false},
		{"mixed frozen", EffectValueMaybeFrozen, EffectValueFrozen, EffectValueMaybeFrozen, false},
		{"frozen primitive", EffectValueFrozen, EffectValuePrimitive, EffectValueFrozen, false},
		{"frozen global", EffectValueFrozen, EffectValueGlobal, EffectValueFrozen, false},
		{"global primitive", EffectValueGlobal, EffectValuePrimitive, EffectValueGlobal, false},
		{"global mutable", EffectValueGlobal, EffectValueMutable, EffectValueMutable, false},
		{"primitive mutable", EffectValuePrimitive, EffectValueMutable, EffectValueMutable, false},
		{"unvisited predecessor", EffectValueFrozen, EffectValueMutable, EffectValueMutable, true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			state := newAliasingState()
			for index, kind := range []EffectValueKind{testCase.left, testCase.right} {
				place := Place{Identifier: IdentifierId(index + 1)}
				state.create(place, aliasingNodeObject)
				if kind != EffectValueMutable {
					state.markImmutable(place.Identifier, kind)
				}
			}
			phi := &Phi{Place: Place{Identifier: 3}, Operands: map[BlockId]Place{
				1: {Identifier: 1}, 2: {Identifier: 2},
			}}
			state.derivePhiImmutable(phi, map[BlockId]bool{1: true, 2: !testCase.unseen})
			if got := state.immutable[3]; got != testCase.want {
				t.Fatalf("phi kind=%s, want %s", got, testCase.want)
			}
		})
	}
}

func TestFreezeRetainsAlreadyImmutableKinds(t *testing.T) {
	t.Parallel()
	for _, kind := range []EffectValueKind{EffectValuePrimitive, EffectValueGlobal, EffectValueFrozen} {
		t.Run(kind.String(), func(t *testing.T) {
			state := newAliasingState()
			place := Place{Identifier: 1}
			state.create(place, aliasingNodeObject)
			state.markImmutable(place.Identifier, kind)
			if state.freeze(place.Identifier) || state.immutable[place.Identifier] != kind {
				t.Fatalf("freeze changed %s to %s", kind, state.immutable[place.Identifier])
			}
		})
	}
}

func TestManualMemoizationPreservesMixedValueClosure(t *testing.T) {
	t.Parallel()
	const source = `import React from 'react';
function Component(props) {
 const items = props.items ?? [];
 const [state,setState] = React.useState(0);
 function findItem(id) { return items.find(item => item.id === id); }
 const item = findItem(props.id);
 consume(item);
 const callback = React.useCallback(() => setState(1), []);
 return <div onClick={callback}>{state}</div>;
}`
	for _, testCase := range []struct {
		name, source string
		fires        bool
	}{
		{"mixed", source, false},
		{"conditional", strings.Replace(source, "props.items ?? []", "props.flag ? props.items : []", 1), false},
		{"frozen", strings.Replace(source, "props.items ?? []", "props.items", 1), false},
		{"mutable", strings.Replace(source, "props.items ?? []", "getItems() ?? []", 1), true},
		{"global", strings.Replace(source, "props.items ?? []", "props.flag ? externalItems : []", 1), true},
		{"primitive mutable", strings.Replace(source, "props.items ?? []", "props.flag ? [] : null", 1), true},
		{"missing dependency", strings.Replace(source, "() => setState(1)", "() => [props.value, setState(1)]", 1), true},
		{"direct method", strings.Replace(source, "function findItem(id) { return items.find(item => item.id === id); }\n const item = findItem(props.id);", "const item = items.find(item => item.id === props.id);", 1), true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			findings, lowered := findingsForSource(t, testCase.source)
			if !lowered || (len(findings) > 0) != testCase.fires {
				t.Fatalf("lowered=%t findings=%v; want fires=%t", lowered, findings, testCase.fires)
			}
		})
	}
}

func TestReadOnlyClosureEffectsRejectUnprovenBodies(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name, body string
		readOnly   bool
	}{
		{"property", "return values.name;", true},
		{"callback", "return values.find(value => value.id === id);", true},
		{"unknown call", "return consume(values);", false},
		{"parameter mutation", "values.name = id; return values;", false},
		{"global assignment", "externalValue = values; return values;", false},
		{"impure builtin", "return Math.random();", false},
		{"nested mutation", "return values.find(value => {value.id = id; return true});", false},
		{"object method", "return {run() {consume(values)}};", false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			function, _ := rangesFor(t, "function helper(values, id) {"+testCase.body+"}")
			if got := hasReadOnlyClosureEffects(function, map[*Function]bool{}); got != testCase.readOnly {
				t.Fatalf("read-only=%t, want %t", got, testCase.readOnly)
			}
		})
	}
}

func TestClosureRefExclusionFollowsDerivedValues(t *testing.T) {
	t.Parallel()
	function, _ := rangesFor(t, `import React from 'react'; function Component(props) {
const reference = React.useRef(null);
const copied = reference;
const current = copied.current;
const nested = current.value;
const selected = props.flag ? nested : null;
const unrelated = props.value;
return {reference, copied, current, nested, selected, unrelated};
}`)
	refs := refDerivedValues(function)
	active := map[IdentifierId]bool{}
	for _, instruction := range function.Instructions {
		if instruction != nil {
			EachInstructionPlace(instruction, func(place Place, role PlaceRole) {
				active[place.Identifier] = true
			})
		}
	}
	for name, want := range map[string]bool{
		"reference": true, "copied": true, "current": true,
		"nested": true, "selected": true, "unrelated": false,
	} {
		found := false
		for _, identifier := range function.Identifiers {
			if identifier != nil && active[identifier.Id] && identifier.Name == name {
				found = true
				if refs[identifier.Id] != want {
					t.Errorf("%s ref-derived=%t, want %t", name, refs[identifier.Id], want)
				}
			}
		}
		if !found {
			t.Errorf("missing %s", name)
		}
	}
}
