// Package gate writes a field it never reads, which unusedwrite reports unless a file of the package imports unsafe.
package gate

// Target writes a field no one reads.
func Target() {
	var x struct{ field int }
	x.field = 1
}
