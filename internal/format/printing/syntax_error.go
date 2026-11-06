package printing

type syntaxError struct{ error }

func (marked syntaxError) Unwrap() error { return marked.error }
