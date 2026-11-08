package differences

import (
	. "strings"
)

func next() int { return 0 }

func key() string { return "" }

func produce() string { return "" }

func current() *embedded { return &embedded{} }

// A selector or index expression whose syntax holds a call is never the same twice for the port, as it is for the
// original: the call is made anew each time and may answer otherwise, and the unconditional form makes it a different
// number of times than the guarded one. The guards are imported with a dot, and each call broken over lines, so each
// record fits on the line its finding starts.
func dynamicIndexes(texts []string, table map[string]string, channel chan int, affix string) {
	// next runs three times when the prefix is there and once when it is not, and twice in the trim.
	if HasPrefix( // port drops: should replace this if statement with an unconditional strings.TrimPrefix
		texts[next()], affix,
	) {
		texts[next()] = texts[next()][len(affix):]
	}
	// key runs once, or three times, where the unconditional call runs it twice.
	if HasPrefix( // port drops: should replace this if statement with an unconditional strings.TrimPrefix
		table[key()], affix,
	) {
		table[key()] = TrimPrefix(table[key()], affix)
	}
	// Each receive takes another value from the channel, so the three indexes may be three.
	if HasSuffix( // port drops: should replace this if statement with an unconditional strings.TrimSuffix
		texts[<-channel], affix,
	) {
		texts[<-channel] = texts[<-channel][:len(texts[<-channel])-len(affix)]
	}
	// A call inside the builtin len is still a call, made anew each time.
	if HasPrefix( // port drops: should replace this if statement with an unconditional strings.TrimPrefix
		texts[len(produce())], affix,
	) {
		texts[len(produce())] = texts[len(produce())][len(affix):]
	}
	// current may hand back another value each time, so the field may be another's.
	if HasPrefix( // port drops: should replace this if statement with an unconditional strings.TrimPrefix
		current().text, affix,
	) {
		current().text = current().text[len(affix):]
	}
}

// A conversion and the builtins len and cap read what they are handed and change nothing, so indexes made of them
// are the same twice, and both still report each guard.
func steadyIndexes(texts []string, position int64, at int, affix string) {
	if HasPrefix(texts[int(position)], affix) {
		texts[int(position)] = texts[int(position)][len(affix):]
	}
	if HasPrefix(texts[(int)(position)], affix) {
		texts[(int)(position)] = TrimPrefix(texts[(int)(position)], affix)
	}
	if HasPrefix(texts[len(affix)], affix) {
		texts[len(affix)] = texts[len(affix)][len(affix):]
	}
	if HasSuffix(texts[cap(texts)-1], affix) {
		texts[cap(texts)-1] = texts[cap(texts)-1][:len(texts[cap(texts)-1])-len(affix)]
	}
	if HasPrefix(texts[at], affix) {
		texts[at] = texts[at][len(affix):]
	}
}
