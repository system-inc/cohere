// Package kept holds a chain of passes a reader asks one end of: Caller passes its slice to Forward, which passes it
// and another to pairs, which checks the first. One question, of Caller's slice, settles Forward's marks and pairs'
// too, and the resolver must hold an answer of its own for each of their parameters all the same, marked or not, as
// it does for the one asked of (#z9fze0t). pairs is unexported, so its parameters have no objectpath, and the resolver
// holds their answers by their objects all the same. Loose is never reached, and has none.
package kept

// pairs panics unless its values come in pairs, and checks neither its label nor the rest.
func pairs(label string, values []string, rest []string) {
	if len(values)%2 != 0 {
		panic(label + ": values come in pairs")
	}
	_ = len(rest)
}

// Forward passes its values on to pairs as the slice pairs checks, and loose as the one it does not.
func Forward(values []string, loose []string) {
	pairs("forward", values, loose)
}

// Caller passes its slice on to Forward.
func Caller(values []string) {
	Forward(values, nil)
}

// Loose takes values of any length, and nothing passes it a parameter.
func Loose(values []string) {
	_ = len(values)
}
