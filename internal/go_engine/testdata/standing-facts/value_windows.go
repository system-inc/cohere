package standing

// broken is windows's error, whose nil pointer value returns.
type broken struct{}

// Error says what broke.
func (*broken) Error() string {
	return "broken"
}

// value is windows's: a nil *broken, which as an error is never nil.
func value() error {
	var failure *broken
	return failure
}
