// Package walk holds the places the port's walk must still reach, and reach once: the port walks the syntax of each
// function of staticcheck's intermediate representation where syntax starts, where the original walks the files.
package walk

// Found twice: in a function literal of a package-level variable, which the package's initializer holds and which has
// no syntax of its own, and in a literal nested inside it.
var packageLiteral = func(source []int, destination []int) []int {
	for _, value := range source {
		destination = append(destination, value)
	}
	nested := func(source []string, destination []string) []string {
		for _, value := range source {
			destination = append(destination, value)
		}
		return destination
	}
	_ = nested
	return destination
}

// Found: a blank-named function, which the representation builds though nothing can call it.
func _(source []int, destination []int) []int {
	for _, value := range source {
		destination = append(destination, value)
	}
	return destination
}

// Found once: a literal inside a declared function, judged in the declared function's walk and not again in its own.
func withLiteral(source []int, destination []int) []int {
	run := func() {
		for index := range source {
			destination = append(destination, source[index])
		}
	}
	run()
	return destination
}

// record holds a slice its method appends to.
type record struct {
	items []int
}

// Found: a method's body.
func (receiver *record) method(source []int) {
	for _, value := range source {
		receiver.items = append(receiver.items, value)
	}
}

// Found once: a generic function's body, built once and never per instantiation.
func genericBody[T any](source []T, destination []T) []T {
	for _, value := range source {
		destination = append(destination, value)
	}
	return destination
}

// Found once each, the generic function called with two types.
func instantiations() {
	_ = genericBody([]int{1}, nil)
	_ = genericBody([]string{"text"}, nil)
}
