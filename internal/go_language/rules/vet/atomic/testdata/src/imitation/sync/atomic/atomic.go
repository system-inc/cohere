// Package atomic imitates sync/atomic under another import path, so parity can call an AddUint64 that is not
// sync/atomic's and see neither side report it.
package atomic

// AddUint64 has sync/atomic's signature and adds nothing atomically.
func AddUint64(address *uint64, delta uint64) uint64 {
	*address += delta
	return *address
}
