package moved

// printedValue is windows's, a string, which `%d` refuses.
type printedValue string

// marshalledValue is windows's, a channel, which never marshals.
type marshalledValue chan int

// writtenValue is windows's, a string, whose size is not fixed.
type writtenValue string
