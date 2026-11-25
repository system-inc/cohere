// Package unexportedmethods reaches foreign's ledger.store and declares a store of the same name and signature on a
// concrete type and on a generic type. An unexported method belongs to its package, where no other package can match
// it, so neither any is fixed.
package unexportedmethods

// Imported only so foreign is in the closure the rule reads.
import _ "unexportedmethods/foreign"

type cache struct{}

func (cache) store(value any) {} // want "as .any."

type queue[Item any] []Item

func (items queue[Item]) store(value any) {} // want "as .any."
