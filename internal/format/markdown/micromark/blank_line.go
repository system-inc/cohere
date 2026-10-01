package micromark

// blankLine is micromark-core-commonmark/lib/blank-line.js.
var blankLine = &Construct{Partial: true, Tokenize: tokenizeBlankLine}

func tokenizeBlankLine(self *Self, effects *Effects, ok State, nok State) State {
	var start, after State

	start = func(code Code) State {
		if markdownSpace(code) {
			return factorySpace(effects, after, TypeLinePrefix, 0)(code)
		}
		return after(code)
	}

	after = func(code Code) State {
		if code == CodeEof || markdownLineEnding(code) {
			return ok(code)
		}
		return nok(code)
	}

	return start
}
