package flagged

import . "fmt"

// A dot import leaves the name bare, and it is still fmt's.
func greet(name string) {
	Println("hello", name) // want `Do not call 'fmt\.Println'`
}
