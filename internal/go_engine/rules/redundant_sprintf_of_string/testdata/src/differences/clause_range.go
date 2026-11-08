package differences

import "fmt"

// The file still uses fmt once every fix of it is applied, so the fixed file is judged by the fixes alone.
var _ = fmt.Sprint

func clauseRange() {
	// The original writes `for range Named{}.String() {`, whose brace the parser takes for the block's.
	for range fmt.Sprintf("%s", Named{}) { // port moves: should use String() instead of fmt.Sprintf
	}
}
