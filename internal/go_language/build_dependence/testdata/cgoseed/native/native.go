// Package native is a cgo package whose C side moves with the tag `wide`, every Go file of it byte for byte the same in
// both builds: the tag adds -DWIDE to the C compiler's flags, and the preamble's macro and struct read WIDE.
package native

/*
#cgo wide CFLAGS: -DWIDE

#ifdef WIDE
#define SLOTS 8
struct shape {
	long long first;
	long long second;
};
#else
#define SLOTS 4
struct shape {
	int first;
};
#endif
*/
import "C"

// Slots is the C macro SLOTS: 4 on the host and 8 under the tag.
const Slots = C.SLOTS

// Shape holds a C struct shape: one int on the host, two long longs under the tag.
type Shape struct {
	Inner C.struct_shape
}
