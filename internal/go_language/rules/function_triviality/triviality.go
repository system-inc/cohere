package function_triviality

// triviality is one function's answer: whether it is trivial, and every other top-level declaration whose body the
// walk read to decide it, each once, sorted by file and offset, nil for none. Its zero value is no fact: not trivial,
// having read no body but the function's own. Its fields are exported for the JSON the resolver keeps an answer as.
type triviality struct {
	Trivial bool
	Entered []declarationPlace
}
