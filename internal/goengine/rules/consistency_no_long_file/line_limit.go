package consistency_no_long_file

import (
	"fmt"
	"strconv"
)

// lineLimit is the limit option's value. It is a flag.Value of its own, rather than flag.Int, so a limit below one,
// which would flag every file or name a line no file has, and a limit above the house's, which would let a file grow
// past the ruling, are refused when it is set.
type lineLimit int

// String is the limit as the flag package prints it, in usage and as the default it compares against.
func (limit *lineLimit) String() string {
	return strconv.Itoa(int(*limit))
}

// Set takes the limit from the text the engine's configuration hands it, refusing what is not a whole number of
// lines from one to the house limit. The engine wraps a refusal with the rule's name, and the refusal names the
// option and the value itself, so it reads whole wherever it is printed.
func (limit *lineLimit) Set(text string) error {
	value, parseError := strconv.Atoi(text)
	if parseError != nil {
		return fmt.Errorf("reading %q as a whole number of lines: %w", text, parseError)
	}
	if value < 1 {
		return fmt.Errorf("refusing a limit of %d lines, since a file holds at least one", value)
	}
	if value > defaultLimit {
		return fmt.Errorf("refusing a %s option of %d lines, since it may only be stricter than the house limit of %d",
			limitOption, value, defaultLimit)
	}
	*limit = lineLimit(value)
	return nil
}
