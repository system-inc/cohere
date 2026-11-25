// Package exportedmethods reaches foreign's Ledger.Cmd and declares a Cmd of the same name and signature on a concrete
// type and on a generic type. A concrete method of another package fixes no name but an interface method's, exported or
// not, so both are ours.
package exportedmethods

// Imported only so foreign is in the closure the rule reads.
import _ "exportedmethods/foreign"

type console struct{}

func (console) Cmd(name string) string { return name } // want `Identifier "Cmd" abbreviates "Cmd"\. Use "Command"`

type queue[Item any] []Item

func (items queue[Item]) Cmd(name string) string { return name } // want `Identifier "Cmd" abbreviates "Cmd"\. Use "Command"`
