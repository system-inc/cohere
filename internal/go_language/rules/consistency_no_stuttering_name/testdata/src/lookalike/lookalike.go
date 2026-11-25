package lookalike

type data struct{ size int }

// Data is a method whose type shares its word.
func (holder data) Data() int { return holder.size }

// A method expression reaches the method through the type, and a type's name is not a placeholder for a value.
func methodExpression() func(data) int {
	return data.Data
}

type inner struct{ Result int }

type holder struct{ result inner }

// The outer word of a member chain is a field, not a placeholder the code reached for, as the TypeScript rule
// leaves this.result.result alone.
func memberChain(container holder) int {
	return container.result.Result
}

// A map key is a lookup, not a member, even when it is spelled as the map is.
func mapKey(result map[string]int) int {
	return result["result"]
}

type account struct{ User string }

type connection struct{ Session int }

// A stutter on a word off the list may be exactly right: a user's User field can say what this one is.
func offTheList(user account, session connection) (string, int) {
	return user.User, session.Session
}

type batch struct {
	Result int
	Values []int
	Item   int
}

// Close is not the same: a plural, a longer word, or a different generic word each says something of its own.
func nearMisses(results batch, value batch, itemCount batch) (int, int, int) {
	return results.Result, len(value.Values), itemCount.Item
}

type keyed struct{ Outcome int }

// A composite literal's key names the field being set; there is no variable in front of it.
func compositeKey() keyed {
	return keyed{Outcome: 1}
}

type région struct{ État string }

// A stutter on a word written in another script is off the list too, and its first letter is lowered rune by rune.
func multiByte(état région) string {
	return état.État
}
