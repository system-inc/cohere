package differences

import "fmt"

// The file still uses fmt once every fix of it is applied, so the fixed file is judged by the fixes alone.
var _ = fmt.Sprint

func clauseIf() {
	// The original writes `if Named{}.String() == "" {`, whose brace the parser takes for the block's.
	if fmt.Sprintf("%s", Named{}) == "" { // port moves: should use String() instead of fmt.Sprintf
	}
}
