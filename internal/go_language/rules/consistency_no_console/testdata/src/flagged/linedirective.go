package flagged

import "fmt"

// A //line directive naming a test file does not make this one a test's: the file a reader opens is linedirective.go.

//line quiet_test.go:1
func renamed() {
	fmt.Println("still a library") // want `Do not call 'fmt\.Println'`
}
