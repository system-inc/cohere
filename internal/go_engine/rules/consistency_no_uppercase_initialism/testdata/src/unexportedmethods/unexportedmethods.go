// Package unexportedmethods reaches foreign's ledger.parseURL and declares a parseURL of the same name and signature on
// a concrete type and on a generic type. Only an interface of another package fixes a name, so both are ours.
package unexportedmethods

// Imported only so foreign is in the closure the rule reads.
import _ "unexportedmethods/foreign"

type console struct{}

func (console) parseURL() string { return "" } // want `"parseURL" .* Use "parseUrl"`

type queue[Item any] []Item

func (items queue[Item]) parseURL() string { return "" } // want `"parseURL" .* Use "parseUrl"`
