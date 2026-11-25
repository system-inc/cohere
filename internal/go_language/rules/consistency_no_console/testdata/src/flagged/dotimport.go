package flagged

import (
	. "fmt"
	. "os"
)

// A dot import leaves the name bare, and it is still fmt's, and the stream still os's.
func greet(name string) {
	Println("hello", name)                 // want `Do not call 'fmt\.Println'`
	_, _ = Fprintln(Stderr, "hello", name) // want `Do not call 'fmt\.Fprintln\(os\.Stderr, \.\.\.\)'`
}
