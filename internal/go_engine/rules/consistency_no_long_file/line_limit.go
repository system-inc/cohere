package consistency_no_long_file

import (
	"fmt"
	"strconv"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// lineLimit is the max option's value. It is a flag.Value of its own, rather than flag.Int, so a limit below one,
// which would flag every file or name a line no file has, is refused when it is set, and so the engine can ask it
// whether it loosens the house limit, which a project may do only with a departure stating why.
type lineLimit int

// The engine finds a loosening option by this interface alone, so a limit that stopped implementing it would loosen
// the house limit with no departure asked for. This fails the build instead.
var _ rule.Loosening = (*lineLimit)(nil)

// String is the limit as the flag package prints it, in usage and as the default it compares against.
func (limit *lineLimit) String() string {
	return strconv.Itoa(int(*limit))
}

// Set takes the limit from the text the engine's configuration hands it, refusing what is not a whole number of
// lines, one or more. A limit above the house's is taken here: whether the settings give a reason for it is the
// engine's check, made for every rule's options alike. The engine wraps a refusal with the rule's name, and the
// refusal names the value itself, so it reads whole wherever it is printed.
func (limit *lineLimit) Set(text string) error {
	value, parseError := strconv.Atoi(text)
	if parseError != nil {
		return fmt.Errorf("reading %q as a whole number of lines: %w", text, parseError)
	}
	if value < 1 {
		return fmt.Errorf("refusing a limit of %d lines, since a file holds at least one", value)
	}
	*limit = lineLimit(value)
	return nil
}

// Loosens is whether the limit lets a file hold more lines than the house limit does, which makes it rule.Loosening.
func (limit *lineLimit) Loosens() bool {
	return int(*limit) > defaultLimit
}

// HouseValue is the house limit as the max option spells it.
func (limit *lineLimit) HouseValue() string {
	return strconv.Itoa(defaultLimit)
}
