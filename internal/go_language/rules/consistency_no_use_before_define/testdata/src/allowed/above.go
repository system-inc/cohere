package allowed

import "strings"

// A var and a const declared above the code that reads them.
var retryLimit = 3

const prefix = "> "

func retries() int {
	return retryLimit
}

func quoted(text string) string {
	return strings.Repeat(prefix, 2) + text
}

// A later spec of a group reads an earlier one, and a later var an earlier var.
const (
	first  = 1
	second = first + 1
)

var doubled = retryLimit * second

// A type used before its definition, and a function called before its definition: Go declares both in any order,
// and reading top down puts the caller first.
func build() *settings {
	return &settings{limit: defaultLimit()}
}

type settings struct{ limit int }

func defaultLimit() int {
	return retryLimit
}

// A spec's value reads a name declared earlier in the same spec.
var start, end = 1, start + 10
