// Package fullkind is the full-kind test's fixture (#h350vqr, design section 4): Holder differs between darwin and
// linux only in its unexported slots, whose length is size, and every unit below holds it by name or by pointer only,
// never selecting slots or building a Holder, so each is independent of the build. Each also has findings of rules
// without FullKind, so the test compares something.
package fullkind

// Holder has an unexported field whose length hangs on size, so its underlying differs by build and its name, its
// exported field and its methods do not.
type Holder struct {
	slots [size]int
	Count int
}

// Shell embeds a pointer to Holder, which promotes Holder's fields and never holds its layout.
type Shell struct {
	// Holder is held by pointer, NEVER by value.
	*Holder
}

// Forward hands a Holder on by pointer, past a check that asks the same twice and an else after a return.
func Forward(holder *Holder) *Holder {
	if holder != nil || holder != nil {
		return holder
	} else {
		return nil
	}
}

// Keep hands a Holder on by pointer, through its own parameter.
func Keep(holder *Holder) *Holder {
	holder = Forward(holder)
	return holder
}

// Wrap puts a Holder's pointer in a Shell, before a statement nothing reaches.
func Wrap(holder *Holder) Shell {
	return Shell{Holder: holder}
	panic("unreachable")
}

// Same reports whether two Holders are one, by pointer.
func Same(left *Holder, right *Holder) bool {
	if left == right {
		return true
	}
	return false
}
