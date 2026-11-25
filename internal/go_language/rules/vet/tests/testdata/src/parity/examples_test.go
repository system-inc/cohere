package parity

import (
	"fmt"
	"testing"

	"other"
)

var _ = other.Helper

// Signatures: each wrong part is its own finding, and the name is judged after them.

func Example_niladic(value int) {}

func Example_results() (count int) { return 0 }

func Example_generic[T any]() {}

func ExampleMissing_all[T any](value int) T { var zero T; return zero }

func ExampleBuffer_Len_both(first, second int) (int, error) { return 0, nil }

// The package's own examples and their suffixes.

func Example_() {}

func Example__twice() {}

func Example_good_Bad() {}

func Example_Ünicode() {}

func Example_über() {}

// Identifiers the package declares, of every kind, and ones it does not.

func ExampleBuffer() {}

func ExampleList() {}

func ExampleMap() {}

func ExampleAlias() {}

func ExamplePair() {}

func ExampleShape() {}

func ExampleDefault() {}

func ExampleLimit() {}

func ExampleCelsius() {}

func Examplehelper() {}

func Examplemissing() {}

func ExampleÜber() {}

func Examples() {}

func ExampleNowhere_Grow() {}

func ExampleNowhere_suffix() {}

// Identifiers only an import declares: testing's T, other's Helper, and fmt's Stringer.

func ExampleT() {}

func ExampleT_Run() {}

func ExampleT_Nope() {}

func ExampleHelper() {}

func ExampleStringer_String() {}

func ExampleStringer_Format() {}

// Members: a field, a promoted method, a pointer method, a value method, an unexported name read as a suffix, and
// ones that do not exist. Buffer is parity's own, so other's Buffer and its Reset are not looked at.

func ExampleBuffer_Size() {}

func ExampleBuffer_Promoted() {}

func ExampleBuffer_Grow() {}

func ExampleBuffer_Len() {}

func ExampleBuffer_inner() {}

func ExampleBuffer_Reset() {}

func ExampleBuffer_() {}

func ExampleBuffer_Missing() {}

func ExampleBuffer_Grow_suffix() {}

func ExampleBuffer_Grow_Bad() {}

func ExampleBuffer_Missing_Bad() {}

func ExampleBuffer_Grow_good_More() {}

func ExampleBuffer_Grow_() {}

func ExampleBuffer_suffix_Bad() {}

func ExampleList_Push() {}

func ExampleList_Head() {}

func ExampleList_Pop() {}

func ExamplePair_Push() {}

func ExamplePair_Pop() {}

func ExampleMap_Keys() {}

func ExampleAlias_Grow() {}

func ExampleAlias_Missing() {}

func ExampleShape_Area() {}

func ExampleShape_Perimeter() {}

func ExampleDefault_Grow() {}

func ExampleDefault_Size() {}

func ExampleLimit_Value() {}

func ExampleCelsius_String() {}

func ExampleCelsius_Kelvin() {}

// Methods are never judged, whatever their names.

func (Buffer) ExampleMissing(value int) int { return value }

// Output comments.

// Output: a doc comment, before the example, is not one of its blocks.
func Example_outputLast() {
	fmt.Println("meow")
	// Output:
	// meow
}

func Example_outputFirst() {
	// Output:
	// meow

	// a comment after it
	fmt.Println("meow")
}

func Example_outputShouting() {
	// OUTPUT: meow

	// a comment after it
}

func Example_outputIndented() {
	//    unordered output: meow

	// a comment after it
}

func Example_outputBlock() {
	/* Output: meow */

	// a comment after it
}

func Example_outputNotOpening() {
	// The Output: here is not at the opening.

	// Output:
	// meow
}

func Example_twoOutputs() {
	// Output: meow

	// Output: meow
}

func Example_threeOutputs() {
	// Output: meow

	// Output: meow

	// Output: meow

	// last
}

func Example_outputInLiteral() {
	print := func() {
		// Output: meow
	}
	print()
	// last
}

func Example_outputWithParameters(value int) {
	// Output: meow

	// last
}

func Example_outputSameLine() { // Output: meow
	// last
}

func Example_outputAfterBrace() {
	// Output: meow
} // Output: bark

func Test_outputNotExample(t *testing.T) {
	// Output: meow

	// last
}
