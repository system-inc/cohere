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

// go test's grammar: after the word, underscores separate UpperCamel words or digits, which -run patterns read.
func TestProduct_BridgeInputProofs(test *testing.T) {}

func TestTokenizerEvents_310(test *testing.T) {}

func Test_Parse(test *testing.T) {}

func TestParser_V2_X(test *testing.T) {}

func BenchmarkParse_Line(benchmark *testing.B) {}

func FuzzParse_Input(fuzz *testing.F) {}

// An example's method segment is the method's own name, whatever its case.
func ExampleResource_URL() {}
