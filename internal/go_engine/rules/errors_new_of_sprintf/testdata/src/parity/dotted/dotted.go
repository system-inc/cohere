package dotted

import (
	. "errors"
	failure "errors"
	"fmt"
	. "fmt"
	format "fmt"
)

// Dot imports and renamed imports all name errors.New and fmt.Sprintf, and the fix spells fmt, as the original's does,
// which names fmt's import here, since the file imports it under its own name too.
func reported(count int) {
	_ = New(Sprintf("%d", count))
	_ = (New)(format.Sprintf("%d", count))
	_ = failure.New(Sprintf("%d", count))
	_ = failure.New(format.Sprintf("%d", count))
	_ = Errorf("%d", count)
	_ = fmt.Errorf("%d", count)
}
