package flagged

// A value declared inside a function is never exported, so a capital first letter on it says what it can't be.
func biggest(Limit int) (Largest int) { // want "`Limit` is .*; spell it `limit`" "`Largest` is .*; spell it `largest`"
	MaxSize := 10 // want "`MaxSize` is declared inside a function and opens with a capital; spell it `maxSize`"

	const HTTPTimeout = 5 // want "`HTTPTimeout` is declared .*; spell it `httpTimeout`"
	var ID int            // want "`ID` is declared .*; spell it `id`"

	for Index, Value := range []int{MaxSize, HTTPTimeout, ID} { // want "`Index` is declared" "`Value` is declared"
		Largest = max(Largest, Index+Value+Limit)
	}
	return Largest
}

type counter struct{ total int }

// A receiver is a parameter of its method, local the same way.
func (Current *counter) add(amount int) { // want "`Current` is declared .*; spell it `current`"
	Current.total += amount
}

func kindOf(subject any) string {
	switch Typed := subject.(type) { // want "`Typed` is declared .*; spell it `typed`"
	case string:
		return Typed
	}
	return ""
}

// A function literal's parameter is local to it.
var double = func(Amount int) int { return Amount * 2 } // want "`Amount` is declared .*; spell it `amount`"
