// Package exportedmethods reaches foreign's Ledger.ParseURL and declares a ParseURL of the same name and signature on a
// concrete type and on a generic type. Only an interface of another package fixes a name, so both are ours.
package exportedmethods

// Imported only so foreign is in the closure the rule reads.
import _ "exportedmethods/foreign"

type console struct{}

func (console) ParseURL() string { return "" } // want `"ParseURL" .* Use "ParseUrl"`

type queue[Item any] []Item

func (items queue[Item]) ParseURL() string { return "" } // want `"ParseURL" .* Use "ParseUrl"`
