// Package dotted imports fmt into its own scope, where Sprintf alone names fmt.Sprintf.
package dotted

import . "fmt"

type number int

func (number) String() string { return "" }

func dotted(text string, value number) {
	_ = Sprintf("%s", text)
	_ = Sprintf("%s", value)
	_ = (Sprintf)("%s", []byte(text))
}
