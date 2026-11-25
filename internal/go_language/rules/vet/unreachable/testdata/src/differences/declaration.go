package differences

import "fmt"

// panic hides the builtin in every file of the package, this one and differences.go.
func panic(message string) {
	fmt.Println(message)
}
