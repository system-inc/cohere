package high_level_intermediate_representation

import "testing"

// TestLowerPrimitiveValueKinds pins that each literal lowers to the variant of its own kind.
//
// The pairs are the ones an untyped value merged: `null` and a missing value were both Go's nil, and
// `1` and `"1"` were both the Go string "1". A lowering that hands either pair one variant fails
// here rather than in whichever rule first asks the question.
func TestLowerPrimitiveValueKinds(test *testing.T) {
	test.Parallel()
	function := lowerSource(test, `function literals() {
  const empty = null;
  const number = 1;
  const text = "1";
  const big = 1n;
  const flag = true;
  return;
}`)
	checkInvariants(test, function)

	want := []PrimitiveValue{
		PrimitiveNull{},
		PrimitiveNumber("1"),
		PrimitiveString("1"),
		PrimitiveBigInt("1n"),
		PrimitiveBoolean(true),
		PrimitiveUndefined{},
	}
	var got []PrimitiveValue
	for _, block := range function.Blocks {
		for _, instructionId := range block.Instructions {
			if primitive, isPrimitive := function.Instructions[instructionId].Value.(*Primitive); isPrimitive {
				got = append(got, primitive.Value)
			}
		}
	}
	if len(got) != len(want) {
		test.Fatalf("lowered %d primitives %#v, want %d %#v:\n%s", len(got), got, len(want), want, Print(function))
	}
	for index := range want {
		if got[index] != want[index] {
			test.Errorf("primitive %d lowered as %#v, want %#v:\n%s", index, got[index], want[index], Print(function))
		}
	}
}
