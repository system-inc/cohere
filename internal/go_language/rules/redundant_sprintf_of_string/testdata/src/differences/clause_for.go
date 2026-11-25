package differences

import "fmt"

// The file still uses fmt once every fix of it is applied, so the fixed file is judged by the fixes alone.
var _ = fmt.Sprint

func clauseFor() {
	// The original writes `for Holder{}.Name != "" {`, whose brace the parser takes for the block's.
	for fmt.Sprintf("%s", Holder{}.Name) != "" { // port moves: the argument is already a string, there's no need to use fmt.Sprintf
		break
	}
}
