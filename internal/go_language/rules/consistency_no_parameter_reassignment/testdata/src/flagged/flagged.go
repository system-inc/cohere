package flagged

import (
	"encoding/json"
	"strconv"
)

type counter struct{ total int }

func assigned(value int) int {
	value = 2 // want "`value` came in from the caller"
	return value
}

func compoundAssigned(value int, label string) (int, string) {
	value += 2   // want "`value` came in from the caller"
	value <<= 1  // want "`value` came in from the caller"
	label += "!" // want "`label` came in from the caller"
	return value, label
}

func incremented(value int, count int) int {
	value++ // want "`value` came in from the caller"
	count-- // want "`count` came in from the caller"
	return value + count
}

// A := at the top of the body shares the parameters' scope, so a name already there is reused, not declared.
func reusedByShortDeclaration(text string) (int, error) {
	text, parsed := text+"0", 0 // want "`text` came in from the caller"
	parsed, parseError := strconv.Atoi(text)
	return parsed, parseError
}

func multipleAssigned(left int, right int) (int, int) {
	left, right = right, left // want "`left` came in" "`right` came in"
	return left, right
}

func parenthesized(value int) int {
	(value) = 3 // want "`value` came in from the caller"
	return value
}

func rangedWithAssignment(index int, item string, items []string) (int, string) {
	for index, item = range items { // want "`index` came in" "`item` came in"
	}
	return index, item
}

func received(value int, channel chan int) int {
	select {
	case value = <-channel: // want "`value` came in from the caller"
	default:
	}
	return value
}

func (receiver *counter) replaced() *counter {
	receiver = &counter{} // want "`receiver` came in from the caller"
	return receiver
}

func (receiver counter) bumped() counter {
	receiver = counter{total: receiver.total + 1} // want "`receiver` came in from the caller"
	return receiver
}

// A function literal reassigning the parameter of the function around it changes that parameter all the same.
func reassignedFromLiteral(value int) int {
	change := func() {
		value = 4 // want "`value` came in from the caller"
	}
	change()
	return value
}

func literalParameter() func(int) int {
	return func(value int) int {
		value *= 2 // want "`value` came in from the caller"
		return value
	}
}

func generic[Element any](value Element, fallback Element) Element {
	value = fallback // want "`value` came in from the caller"
	return value
}

func addressPassed(data []byte, options counter) (counter, error) {
	decodeError := json.Unmarshal(data, &options) // want "takes the address of `options`"
	return options, decodeError
}

func (receiver counter) addressReturned() *counter {
	return &receiver // want "takes the address of `receiver`"
}

func addressParenthesized(value int) *int {
	return &(value) // want "takes the address of `value`"
}

func multiByte(größe int) int {
	größe = 5 // want "`größe` came in from the caller"
	return größe
}
