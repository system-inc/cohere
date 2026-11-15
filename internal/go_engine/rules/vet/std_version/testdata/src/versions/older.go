//go:build go1.21

// Package versions is std_version's fixture for the unit-isolation test (internal/go_engine/check): functions in files
// whose build lines set versions older than the standard library symbols they use, calling one another, so a variant
// that stubs a callee and keeps its caller shows whether the caller's findings rest on its own identifiers alone. The
// package lies outside a module, so it has no version of its own, and only a file's build line gives it one.
package versions

import (
	"cmp"
	"reflect"
	"slices"
	"strings"
	"sync"
)

// joined uses slices.Concat, from go1.22, newer than this file's go1.21.
func joined(left []string, right []string) []string {
	return slices.Concat(left, right)
}

// named uses cmp.Or, from go1.22.
func named(given string) string {
	return cmp.Or(given, "unnamed")
}

// typeOf uses reflect.TypeFor, from go1.22, instantiated, which is judged as the generic function it instantiates.
func typeOf[Kind any]() reflect.Type {
	return reflect.TypeFor[Kind]()
}

// cleared uses (*sync.Map).Clear, from go1.23, a method newer than its type.
func cleared(table *sync.Map) {
	table.Clear()
}

// described calls each of the above, and uses nothing too new itself.
func described(table *sync.Map) string {
	cleared(table)
	return strings.Join(joined([]string{named("")}, nil), ",") + typeOf[int]().String()
}

// trimmed uses only strings.Cut, from go1.18, older than this file, so it reports nothing.
func trimmed(text string) string {
	before, _, _ := strings.Cut(text, "=")
	return before
}
