package policy

// MessageHandle is a rule's claim on one of its messages, taken when the rule's package initializes.
type MessageHandle struct {
	Rule string
	Id   string
}

// Option is a rule's claim on one option of an object phrase this message uses, taken in a
// package-level var like the handle. An option the catalog does not hold for this message panics.
func (handle MessageHandle) Option(phrase string, name string) MessageOption {
	return currentMessages.Load().Option(handle, phrase, name)
}

// Render is the message's text for its rule's language, with one option chosen for each object phrase
// and values put in. It reads the current catalog, so a test that swapped in an edited copy sees the
// edit. A missing or extra option, or a value the text does not use or uses and was not given, panics:
// a finding reading `{{constructor}}` or `<<reason>>` is the silent version.
func (handle MessageHandle) Render(values map[string]string, options ...MessageOption) string {
	return currentMessages.Load().Render(handle, values, options...)
}
