//go:build !windows

package moved

// printedValue is every platform's but windows's, an int, which `%d` prints.
type printedValue int

// marshalledValue is every platform's but windows's, an int, which marshals.
type marshalledValue int

// writtenValue is every platform's but windows's, an int32, whose size is fixed.
type writtenValue int32
