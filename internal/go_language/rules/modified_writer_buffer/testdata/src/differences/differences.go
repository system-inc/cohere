// Package differences holds the cases where the port and the original could plausibly disagree, and records no
// difference, since there is none: both read the same intermediate representation. Stores built from a tuple
// assignment or an increment might be built with syntax of another shape, or none; a method's signature might be
// spelled through aliases; and a buffer may be stored into on a path the builder never reaches.
package differences

type tuple struct{}
type swap struct{}
type increment struct{}
type unreachable struct{}
type neverTaken struct{}
type spelled struct{}
type blank struct{}
type unnamed struct{}

type byteAlias = byte
type errorAlias = error

// Found by both, twice: a tuple assignment storing two elements.
func (tuple) Write(buffer []byte) (int, error) {
	buffer[0], buffer[1] = 'a', 'b'
	return len(buffer), nil
}

// Found by both, twice: a swap of two elements.
func (swap) Write(buffer []byte) (int, error) {
	buffer[0], buffer[1] = buffer[1], buffer[0]
	return len(buffer), nil
}

// Found by both: an increment of an element.
func (increment) Write(buffer []byte) (int, error) {
	buffer[0]++
	return len(buffer), nil
}

// Found by neither: the store is unreachable, after a return.
func (unreachable) Write(buffer []byte) (int, error) {
	return len(buffer), nil
	buffer[0] = 0
	return 0, nil
}

// Found by both: the store is inside a branch never taken, which the builder still builds.
func (neverTaken) Write(buffer []byte) (int, error) {
	if false {
		buffer[0] = 0
	}
	return len(buffer), nil
}

// Found by both: the signature spelled through aliases.
func (spelled) Write(buffer []byteAlias) (count int, failure errorAlias) {
	buffer[0] = 0
	return len(buffer), nil
}

// Found by neither: a blank receiver and a blank buffer, which nothing can write into by name.
func (_ blank) Write(_ []byte) (int, error) {
	return 0, nil
}

// Found by neither: an unnamed buffer.
func (unnamed) Write([]byte) (int, error) {
	return 0, nil
}
