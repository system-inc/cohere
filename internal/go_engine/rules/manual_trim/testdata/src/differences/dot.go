package differences

import (
	. "strings"
)

// A guard imported with a dot is named by its package's path all the same, and reported.
func dotImported(text string, affix string) {
	if HasPrefix(text, affix) {
		text = text[len(affix):]
	}
	if HasSuffix(text, affix) {
		text = TrimSuffix(text, affix)
	}
	_ = text
}
