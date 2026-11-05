package allowed

import (
	"fmt"
	"log"
	"testing"
)

// A test's output is read by the person running the tests, so a test file prints.
func TestDescribe(test *testing.T) {
	test.Parallel()
	text, _ := describe(3)
	fmt.Println(text)
	log.Printf("described %q", text)
	println("done")
}
