package differences

import (
	"fmt"
)

type sprinter func(values ...any) string

// The shapes where the two could most plausibly part, which record nothing, since the port parts from the original
// nowhere.
func shapes() {
	// A literal whose text is longer than its value, and a raw one across lines, are replaced by the text as written.
	_ = fmt.Sprint("tab\there \"quoted\"")
	_ = fmt.Sprint(`first
second`)

	// A backslash before x25 spells no percent sign in a raw literal or an escaped one, so both are reported.
	_ = fmt.Sprintf(`\x25d`)
	_ = fmt.Sprintf("\\x25d")

	// A conversion to a function type is a call whose callee is no function.
	_ = sprinter(fmt.Sprint)("converted")

	// A call in a function literal, and a call as a composite literal's element.
	_ = func() string { return fmt.Sprintf("literal") }()
	_ = []string{fmt.Sprint("element")}

	// A local fmt shadows the import, and its Sprint is a field.
	{
		fmt := struct{ Sprint func(...any) string }{}
		_ = fmt.Sprint("field")
	}
}
