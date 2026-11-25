package differences

import "fmt"

// The file still uses fmt once every fix of it is applied, so the fixed file is judged by the fixes alone.
var _ = fmt.Sprint

func clauseInit() {
	// The original writes `if text := Named{}.String(); text != "" {`, whose brace the parser takes for the block's.
	if text := fmt.Sprintf("%s", Named{}); text != "" { // port moves: should use String() instead of fmt.Sprintf
	}
}
