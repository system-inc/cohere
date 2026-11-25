// Package parity holds every shape the pattern accepts or refuses: the operators between the two calls, the functions
// on each side and whether they agree, parentheses around the calls, their functions and their arguments, calls of
// more or fewer arguments, functions held in variables and functions of other packages, and the strings package
// imported under another name or into the file's own scope, in files of their own.
package parity

import (
	"bytes"
	"strings"
)

type text string

func operators(first string, second string) {
	_ = strings.ToLower(first) == strings.ToLower(second)
	_ = strings.ToLower(first) != strings.ToLower(second)
	_ = strings.ToUpper(first) == strings.ToUpper(second)
	_ = strings.ToUpper(first) != strings.ToUpper(second)
	_ = strings.ToLower(first) < strings.ToLower(second)
	_ = strings.ToLower(first) >= strings.ToLower(second)
	_ = strings.ToLower(first) + strings.ToLower(second)
	_ = strings.ToLower(first) == strings.ToUpper(second)
	_ = strings.ToUpper(first) == strings.ToLower(second)
	_ = strings.ToLower(first) == second
	_ = first == strings.ToLower(second)
	_ = strings.ToTitle(first) == strings.ToTitle(second)
	_ = strings.TrimSpace(first) == strings.TrimSpace(second)
}

func parentheses(first string, second string) {
	_ = (strings.ToLower(first)) == strings.ToLower(second)
	_ = strings.ToLower(first) == (strings.ToLower(second))
	_ = (strings.ToLower)(first) == strings.ToLower(second)
	_ = strings.ToLower(first) == (strings.ToLower)(second)
	_ = strings.ToLower((first)) == strings.ToLower((second))
	_ = (strings.ToLower(first) == strings.ToLower(second))
	_ = (strings.ToLower(first) != strings.ToLower(second)) == true
}

func arguments(first string, second string, value text, values []string) {
	_ = strings.ToLower(first+"a") == strings.ToLower(second[1:])
	_ = strings.ToLower(string(value)) == strings.ToLower(values[0])
	_ = strings.ToLower(strings.TrimSpace(first)) == strings.ToLower("CONSTANT")
	_ = strings.ToLower(strings.ToLower(first)) == strings.ToLower(second)
}

func functionsElsewhere(first string, second string, data []byte, other []byte) {
	lower := strings.ToLower
	_ = lower(first) == lower(second)
	_ = lower(first) == strings.ToLower(second)
	_ = strings.ToLower(first) == lower(second)
	_ = bytes.Equal(bytes.ToLower(data), bytes.ToLower(other))
	_ = string(bytes.ToLower(data)) == string(bytes.ToLower(other))
}

func statements(first string, second string) bool {
	if strings.ToUpper(first) != strings.ToUpper(second) {
		return false
	}
	switch {
	case strings.ToLower(first) == strings.ToLower(second):
		return true
	}
	return strings.ToLower(first) == strings.ToLower(second) && first != ""
}

func generic[S ~string](first S, second S) bool {
	return strings.ToLower(string(first)) == strings.ToLower(string(second))
}
