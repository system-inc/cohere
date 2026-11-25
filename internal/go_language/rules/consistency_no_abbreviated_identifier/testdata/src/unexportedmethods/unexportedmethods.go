// Package unexportedmethods reaches foreign's ledger.cmd and declares a cmd of the same name and signature on a
// concrete type and on a generic type. A concrete method of another package fixes no name but an interface method's,
// so both are ours.
package unexportedmethods

// Imported only so foreign is in the closure the rule reads.
import _ "unexportedmethods/foreign"

type console struct{}

func (console) cmd(name string) string { return name } // want `Identifier "cmd" should not be abbreviated\. Use "command"`

type queue[Item any] []Item

func (items queue[Item]) cmd(name string) string { return name } // want `Identifier "cmd" should not be abbreviated\. Use "command"`
