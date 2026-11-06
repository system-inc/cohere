// Package lookalike holds lines that look past the limit and are not.
package lookalike

func use(arguments ...any) int { return len(arguments) }

func cases(argument string, name string) {
	if argument != name {
		for index := range use(argument, name) {
			// The next line is exactly 120 columns, each of its three tabs counted as four, so it is at the limit.
			_ = use(index, argument, name, "and so on and so on and so on and so on and so on and so on and so on andx")
		}
	}
	// The next line is under 120 runes and over 120 bytes, since each of its letters is three bytes in UTF-8.
	_ = use(argument, "日本語の文字列は一文字が三バイトなので日本語の文字列は一文字が三バイトなので日本語の文字列は一文字が三バイトなので")
	// The next line is a comment that ends at exactly 120 columns, so it is at the limit, not past it.
	// and so on and so on and so on and so on and so on and so on and so on and so on and so on and so on and so on and
}
