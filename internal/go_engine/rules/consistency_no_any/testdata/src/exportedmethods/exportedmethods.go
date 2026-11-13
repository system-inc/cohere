// Package exportedmethods reaches foreign's Ledger.Store and declares a Store of the same name and signature on a
// concrete type and on a generic type. A concrete method of another package fixes no concrete method of ours, which Go
// relates to it by no rule (#9bm8bdt), so neither any is fixed.
package exportedmethods

// Imported only so foreign is in the closure the rule reads.
import _ "exportedmethods/foreign"

type cache struct{}

func (cache) Store(value any) {} // want "as .any."

type queue[Item any] []Item

func (items queue[Item]) Store(value any) {} // want "as .any."
