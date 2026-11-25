// Package parity holds Write methods of io.Writer's signature and of others, and the ways a method can touch its
// buffer, written into, appended onto, copied into, resliced, captured or only read, where the port and staticcheck's
// SA1023 still agree finding for finding. The differential test runs both over each and compares.
package parity

type appender struct{}
type storer struct{}
type pointerReceiver struct{}
type looper struct{}
type twice struct{}
type appendSelf struct{}
type discarded struct{}
type compound struct{}
type throughCopy struct{}
type namedResults struct{}
type selfStore struct{}
type generic[T any] struct{ value T }

type copier struct{}
type resliced struct{}
type appendResliced struct{}
type captured struct{}
type addressed struct{}
type reader struct{}
type lowercase struct{}
type variadic struct{}
type threeResults struct{}
type inner struct{}

type Bytes []byte

// Found by both: an append onto the buffer.
func (appender) Write(buffer []byte) (int, error) {
	buffer = append(buffer, '\n')
	return len(buffer), nil
}

// Found by both: a store into an element.
func (storer) Write(buffer []byte) (int, error) {
	buffer[0] = 'x'
	return len(buffer), nil
}

// Found by both: a pointer receiver.
func (*pointerReceiver) Write(buffer []byte) (int, error) {
	buffer[0] = 'x'
	return len(buffer), nil
}

// Found by both: a store in a loop over the buffer.
func (looper) Write(buffer []byte) (int, error) {
	for index := range buffer {
		buffer[index] = 0
	}
	return len(buffer), nil
}

// Found by both, twice: two stores.
func (twice) Write(buffer []byte) (int, error) {
	buffer[0] = 'a'
	buffer[1] = 'b'
	return len(buffer), nil
}

// Found by both: the buffer appended onto itself.
func (appendSelf) Write(buffer []byte) (int, error) {
	buffer = append(buffer, buffer...)
	return len(buffer), nil
}

// Found by both: an append whose result is thrown away.
func (discarded) Write(buffer []byte) (int, error) {
	_ = append(buffer, 0)
	return len(buffer), nil
}

// Found by both: a compound assignment to an element.
func (compound) Write(buffer []byte) (int, error) {
	buffer[0] += 1
	return len(buffer), nil
}

// Found by both: a store through another name for the same slice.
func (throughCopy) Write(buffer []byte) (int, error) {
	alias := buffer
	alias[0] = 0
	return len(buffer), nil
}

// Found by both: named results.
func (namedResults) Write(buffer []byte) (written int, failure error) {
	buffer[0] = 0
	return len(buffer), nil
}

// Found by both: an element stored with its own value.
func (selfStore) Write(buffer []byte) (int, error) {
	buffer[0] = buffer[0]
	return len(buffer), nil
}

// Found by both: a method of a generic type.
func (value generic[T]) Write(buffer []byte) (int, error) {
	buffer[0] = 0
	return len(buffer), nil
}

// Found by neither: copy writes into the buffer, but it is neither a store nor an append.
func (copier) Write(buffer []byte) (int, error) {
	return copy(buffer, "overwritten"), nil
}

// Found by neither: the store is into a slicing of the buffer, another value.
func (resliced) Write(buffer []byte) (int, error) {
	rest := buffer[1:]
	rest[0] = 0
	return len(buffer), nil
}

// Found by neither: the append is onto a slicing of the buffer.
func (appendResliced) Write(buffer []byte) (int, error) {
	buffer = append(buffer[:0], 'x')
	return len(buffer), nil
}

// Found by neither: the buffer is captured by a literal that writes into it, so it lives in memory.
func (captured) Write(buffer []byte) (int, error) {
	overwrite := func() { buffer[0] = 0 }
	overwrite()
	return len(buffer), nil
}

// Found by both: the buffer's address is taken, but the pointer never leaves the method, so the builder reads the
// store through it as a store into the parameter.
func (addressed) Write(buffer []byte) (int, error) {
	pointer := &buffer
	(*pointer)[0] = 0
	return len(buffer), nil
}

// Found by neither: the buffer is only read.
func (reader) Write(buffer []byte) (int, error) {
	total := 0
	for _, value := range buffer {
		total += int(value)
	}
	return total, nil
}

// Found by neither: a method named write, not Write.
func (lowercase) write(buffer []byte) (int, error) {
	buffer[0] = 0
	return len(buffer), nil
}

// Found by neither: a variadic buffer, another signature.
func (variadic) Write(buffer ...byte) (int, error) {
	buffer[0] = 0
	return len(buffer), nil
}

// Found by neither: three results, another signature.
func (threeResults) Write(buffer []byte) (int, error, bool) {
	buffer[0] = 0
	return len(buffer), nil, true
}

// Found by neither: a named byte slice, another signature.
func (Bytes) Write(buffer Bytes) (int, error) {
	buffer[0] = 0
	return len(buffer), nil
}

// Found by neither: a function named Write, no method.
func Write(buffer []byte) (int, error) {
	buffer[0] = 0
	return len(buffer), nil
}

// Found by neither: the literal inside a Write method is a function of its own, named for the method but not Write.
func (inner) Write(buffer []byte) (int, error) {
	other := []byte("copy")
	func(buffer []byte) {
		buffer[0] = 0
	}(other)
	return len(buffer), nil
}
