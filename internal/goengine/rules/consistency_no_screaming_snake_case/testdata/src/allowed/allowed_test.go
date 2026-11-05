package allowed

import (
	"fmt"
	"testing"
)

func TestParseLine(test *testing.T) {}

func BenchmarkParseLine(benchmark *testing.B) {}

func FuzzParseInput(fuzz *testing.F) {}

// go doc reads an example's name whole: the type, the method, and a suffix naming one of several.
func Example_basic() {
	fmt.Println("basic")
	// Output: basic
}

func ExampleHttpClient_SendRequest() {}

func ExampleHttpClient_SendRequest_withRetries() {}
