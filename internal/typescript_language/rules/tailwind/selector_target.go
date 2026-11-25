package tailwind

import (
	"encoding/json"
	"fmt"
	"math"
)

// SelectorTarget is a call or argument target: all, first, last, or an index, negative from the end.
type SelectorTarget struct {
	Word  string
	Index int
}

// UnmarshalJSON reads a target as upstream's schema does: one of three words, or a number. A number
// that is not an integer is refused rather than read, since upstream indexes with it and crashes.
func (target *SelectorTarget) UnmarshalJSON(raw []byte) error {
	var word string
	if decodeError := json.Unmarshal(raw, &word); decodeError == nil {
		switch word {
		case "all", "first", "last":
			*target = SelectorTarget{Word: word}
			return nil
		}
		return fmt.Errorf("a target is all, first, last or a number, not %q", word)
	}
	var number float64
	if decodeError := json.Unmarshal(raw, &number); decodeError != nil {
		return fmt.Errorf("a target is all, first, last or a number, not %s", raw)
	}
	if number != math.Trunc(number) || math.Abs(number) > 1<<31 {
		return fmt.Errorf("a target index must be an integer, not %s", raw)
	}
	*target = SelectorTarget{Index: int(number)}
	return nil
}

// MarshalJSON writes a target back in its upstream form, so a settings key built from selectors reads
// the same target the same way.
func (target SelectorTarget) MarshalJSON() ([]byte, error) {
	if target.Word != "" {
		return json.Marshal(target.Word)
	}
	return json.Marshal(target.Index)
}
