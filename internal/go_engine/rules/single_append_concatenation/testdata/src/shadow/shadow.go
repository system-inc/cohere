// Package shadow declares an append of its own at package scope, which is not the universe's, so neither side
// reports a loop that calls it.
package shadow

func append(slice []int, element int) []int {
	return slice[:len(slice):len(slice)]
}

func loops(source []int, destination []int) []int {
	for _, element := range source {
		destination = append(destination, element)
	}
	for index := range source {
		destination = append(destination, source[index])
	}
	return destination
}
