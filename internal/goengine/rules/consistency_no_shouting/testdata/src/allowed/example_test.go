package allowed

import "fmt"

// An example's output is what it prints, so the last comment group in its body, opening with Output: or Unordered
// output:, is not read.

func Example() {
	fmt.Println("BARE")
	// Output: BARE
}

func ExampleThing() {
	fmt.Println("HELLO WORLD")
	// Output: HELLO WORLD
}

func ExampleThing_lines() {
	fmt.Println("LOUD")
	fmt.Println("NOISE")
	// Output:
	// LOUD
	// NOISE
}

func ExampleOlder() {
	fmt.Println("NOISE")
	fmt.Println("LOUD")
	// Unordered output:
	// LOUD
	// NOISE
}
