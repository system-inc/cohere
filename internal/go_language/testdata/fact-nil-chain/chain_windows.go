package nilchain

// platformError is windows's: a nil *failure, which as an error is never nil.
func platformError() error {
	var broken *failure
	return broken
}
