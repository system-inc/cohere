package parity

import format "fmt"

// A renamed import names fmt.Sprintf as well.
func renamed(text string, number Number, bytes Bytes) {
	_ = format.Sprintf("%s", text)
	_ = format.Sprintf("%s", number)
	_ = format.Sprintf("%s", bytes)
}
