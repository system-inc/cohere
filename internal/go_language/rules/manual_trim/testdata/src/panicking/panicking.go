// Package panicking holds the guards whose comparison makes astutil.Equal panic, so the original crashes on this
// package and only the port is run over it. The port declines each, and reports nothing.
package panicking

import "strings"

func guards(texts []string, affix string) {
	// The index holds a function type, a node kind astutil.Equal does not know.
	if strings.HasPrefix(texts[len([]func(){})], affix) {
		texts[len([]func(){})] = texts[len([]func(){})][len(affix):]
	}
	// One struct field has a tag and the other none, and astutil.Equal reads the absent tag.
	if strings.HasPrefix(texts[len([]struct {
		number int "tag"
	}{})], affix) {
		texts[len([]struct{ number int }{})] = texts[len([]struct{ number int }{})][len(affix):]
	}
}
