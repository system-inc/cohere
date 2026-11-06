package compose

// errorSource is what upstream's ComposeErrorHandler is handed to place an error: an errorOffset, an
// errorPosition, an errorRange or an errorToken. Its one method is upstream's getErrorPos for that
// variant, and being unexported it seals the set to these four.
type errorSource interface {
	position() [2]int
}
