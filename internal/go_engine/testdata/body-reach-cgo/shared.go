// Package gate writes a field it never reads, which unusedwrite reports unless the package imports unsafe, as a
// translated cgo file does.
package gate

// Target writes a field no one reads.
func Target() {
	var x struct{ field int }
	x.field = 1
}
