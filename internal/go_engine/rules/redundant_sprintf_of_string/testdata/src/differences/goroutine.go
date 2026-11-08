package differences

import "fmt"

func goroutine(text string) {
	// Go takes a call, and the argument alone is none, which the parser refuses.
	go fmt.Sprintf("%s", text) // port withholds: Remove unnecessary call to fmt.Sprintf
}
