package lookalike

import "fmt"

// go/doc matches the output marker in any case, so a lowercase one still holds the example's output.

func ExampleTagged() {
	fmt.Println("LOUD")
	// output: LOUD
}

func ExampleTagged_indented() {
	fmt.Println("NOISE")
	//   Output: NOISE
}
