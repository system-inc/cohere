package high_level_intermediate_representation

import "testing"

// TestDeepCopyKeepsTheTypeItWasHanded pins the two halves of deepCopy's contract a caller relies on
// without asserting: a nil interface comes back nil, and a value comes back as storage of its own.
func TestDeepCopyKeepsTheTypeItWasHanded(test *testing.T) {
	test.Parallel()

	var missing Terminal
	if copied := deepCopy(missing); copied != nil {
		test.Errorf("a nil terminal copied to %#v, want nil", copied)
	}

	original := &If{Test: Place{Identifier: 3}, Consequent: 1, Alternate: 2}
	var terminal Terminal = original
	copied, isIf := deepCopy(terminal).(*If)
	if !isIf {
		test.Fatalf("an *If copied to %T, want *If", deepCopy(terminal))
	}
	if copied == original {
		test.Fatal("the copy is the original pointer, so a rewrite through it renames the original")
	}
	if *copied != *original {
		test.Errorf("the copy holds %#v, want %#v", *copied, *original)
	}

	// A literal's variant survives the copy, through the interface field `Primitive.Value` holds.
	primitive, isPrimitive := deepCopy[InstructionValue](&Primitive{Value: PrimitiveNumber("1")}).(*Primitive)
	if !isPrimitive || primitive.Value != PrimitiveNumber("1") {
		test.Errorf("a number primitive copied to %#v, want PrimitiveNumber(\"1\")", primitive)
	}
}
