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

// quietFailed is windows's alone, so the build lints it, and it asks Quiet's nil-ness, which the host never asked.
func quietFailed() bool {
	return Quiet() != nil
}

// viaHidden returns Hidden's error, so judging it keeps an answer about Hidden.
func viaHidden() error {
	return Hidden()
}

// hiddenFailed is windows's alone, and asks viaHidden's nil-ness, whose judgment reads Hidden's.
func hiddenFailed() bool {
	return viaHidden() != nil
}
