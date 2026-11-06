package main

// textStyle colors text for a terminal, and does nothing when the output is not one or NO_COLOR is set,
// so a log, a pipe or a reader who asked for no color gets no escape codes at all.
type textStyle struct {
	color bool
}

func (style textStyle) wrap(text, open, close string) string {
	if !style.color || text == "" {
		return text
	}
	return open + text + close
}

func (style textStyle) bold(text string) string { return style.wrap(text, "\x1b[1m", "\x1b[22m") }

func (style textStyle) dim(text string) string { return style.wrap(text, "\x1b[2m", "\x1b[22m") }

func (style textStyle) red(text string) string { return style.wrap(text, "\x1b[31m", "\x1b[39m") }
