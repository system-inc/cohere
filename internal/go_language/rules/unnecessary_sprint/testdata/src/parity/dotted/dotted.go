package dotted

import (
	. "fmt"
)

// A dot import names fmt.Sprint and fmt.Sprintf without a selector.
func reported() {
	_ = Sprint("dotted")
	_ = (Sprintf)("dotted")
}

// A local spelled Sprint shadows the dot import, and is no fmt.Sprint.
func shadowed() {
	Sprint := func(values ...any) string { return Sprintln(values...) }
	_ = Sprint("shadowed")
}
