// Package differences holds the cases where the port and the original could plausibly disagree, and records no
// difference, since there is none: both read the same intermediate representation. A conversion from a type parameter
// is built as one conversion per term of its type set, and a type set may be any of several shapes; a lookup may sit
// in a function literal, deferred or run as a goroutine; and a conversion may be read by one lookup twice over.
package differences

type bytesAlias = []byte

type namedBytes []byte

// Found by both: an alias of a byte slice.
func alias(lookup map[string]int, data bytesAlias) int {
	converted := string(data)
	return lookup[converted]
}

// Found by both: a type parameter whose set is a union of two named byte slices.
func genericUnion[T namedBytes | bytesAlias](lookup map[string]int, data T) int {
	converted := string(data)
	return lookup[converted]
}

type byteLike interface{ ~[]byte }

type byteOrString interface {
	byteLike | ~string
}

// Found by both: a type parameter constrained by an interface that embeds another.
func genericEmbedded[T byteOrString](lookup map[string]int, data T) int {
	converted := string(data)
	return lookup[converted]
}

// Found by neither: a type parameter whose set is every type, which holds no term to judge.
func genericAny[T any](lookup map[string]int, data T) int {
	return lookup[""]
}

// Found by both, once each: the conversion and its lookup inside a deferred literal and a goroutine's.
func literals(lookup map[string]int, data []byte) {
	defer func() {
		converted := string(data)
		_ = lookup[converted]
	}()
	go func() {
		converted := string(data)
		_ = lookup[converted]
	}()
}

// Found by both: a package variable's initializer, built in the package's init.
var initialized = func() int {
	data := []byte("key")
	lookup := map[string]int{}
	converted := string(data)
	return lookup[converted]
}()

// Found by neither: the lookup is unreachable, after a return, so nothing reads the conversion.
func lookupAfterReturn(lookup map[string]int, data []byte) int {
	converted := string(data)
	return 0
	return lookup[converted]
}

// Found by both: the lookup inside a branch never taken.
func neverTaken(lookup map[string]int, data []byte) int {
	converted := string(data)
	if false {
		return lookup[converted]
	}
	return 0
}
