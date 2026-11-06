package allowed

type holder struct{ total int }

// The compound form and the increment are what the rule asks for.
func compound(count int, step int, text string, item *holder, values []int) {
	count += step
	count <<= 2
	count &^= step
	count++
	text += "!"
	item.total -= 1
	values[0] *= 2
	_, _ = count, text
}

// An assignment that does not read its own target has no compound form.
func otherValues(count int, step int, other int) {
	count = other + step
	count = step
	count = other * other
	_ = count
}

// A declaration, a tuple assignment and the blank identifier are not the assignment the rule rewrites.
func notOneTarget(count int, other int) {
	total := count + 1
	count, other = count+1, other
	_ = count + 1
	_, _, _ = total, count, other
}

// Logical and comparison operators have no compound form in Go.
func noCompoundForm(ready bool, done bool, count int, step int) {
	ready = ready && done
	ready = ready || done
	ready = ready == done
	ready = count < step
	_ = ready
}
