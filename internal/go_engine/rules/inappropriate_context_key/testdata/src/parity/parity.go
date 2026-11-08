// Package parity holds keys of each kind of type context.WithValue could be given: built in types, their aliases
// here and in another package and the types defined from them, structs empty and not, named and not, types that
// cannot be compared, interfaces holding a key and nil, pointers, type parameters, and the shapes staticcheck's
// representation builds the call in more than one way.
package parity

import (
	"context"
	"unsafe"

	"keys"
)

type name string

type number = int

type aliasedName = name

type empty struct{}

type emptyAlias = struct{}

type withSlice struct{ values []int }

type withArray struct{ values [2]int }

type withMap struct{ table map[string]int }

type withFunction struct{ callback func() }

type listed []int

type keyInterface interface{ key() }

var global = context.WithValue(context.Background(), "global", nil)

func builtIn(parent context.Context, text string, flag bool, real float64, pointer unsafe.Pointer) {
	context.WithValue(parent, text, nil)
	context.WithValue(parent, 1, nil)
	context.WithValue(parent, int64(1), nil)
	context.WithValue(parent, uint8(1), nil)
	context.WithValue(parent, flag, nil)
	context.WithValue(parent, real, nil)
	context.WithValue(parent, 1.5, nil)
	context.WithValue(parent, 'r', nil)
	context.WithValue(parent, complex(1, 2), nil)
	context.WithValue(parent, pointer, nil)
	context.WithValue(parent, uintptr(0), nil)
	context.WithValue(parent, true, nil)
	context.WithValue(parent, `raw`, nil)
}

func aliases(parent context.Context, aliased number, imported keys.Name, named aliasedName) {
	context.WithValue(parent, aliased, nil)
	context.WithValue(parent, number(2), nil)
	context.WithValue(parent, imported, nil)
	context.WithValue(parent, keys.Name("k"), nil)
	context.WithValue(parent, named, nil)
	var byteKey byte = 1
	context.WithValue(parent, byteKey, nil)
	var runeKey rune = 'r'
	context.WithValue(parent, runeKey, nil)
	var anyKey any = "text"
	context.WithValue(parent, anyKey, nil)
}

func defined(parent context.Context, key name, pointer *int, function func()) {
	context.WithValue(parent, key, nil)
	context.WithValue(parent, name("k"), nil)
	context.WithValue(parent, &key, nil)
	context.WithValue(parent, pointer, nil)
	context.WithValue(parent, function, nil)
	context.WithValue(parent, keyInterface(nil), nil)
	context.WithValue(parent, nil, nil)
	context.WithValue(parent, any(nil), nil)
	context.WithValue(parent, make(chan int), nil)
}

func structs(parent context.Context) {
	context.WithValue(parent, struct{}{}, nil)
	context.WithValue(parent, empty{}, nil)
	context.WithValue(parent, emptyAlias{}, nil)
	context.WithValue(parent, keys.Empty{}, nil)
	context.WithValue(parent, struct{ name string }{"k"}, nil)
	context.WithValue(parent, struct{ values []int }{}, nil)
	context.WithValue(parent, withSlice{}, nil)
	context.WithValue(parent, withArray{}, nil)
	context.WithValue(parent, withMap{}, nil)
	context.WithValue(parent, withFunction{}, nil)
	context.WithValue(parent, &withSlice{}, nil)
	context.WithValue(parent, [2]int{}, nil)
	context.WithValue(parent, [2][]int{}, nil)
}

func notComparable(parent context.Context, list listed, table map[string]int, function func(), values []string) {
	context.WithValue(parent, list, nil)
	context.WithValue(parent, table, nil)
	context.WithValue(parent, values, nil)
	context.WithValue(parent, values[0], nil)
	context.WithValue(parent, func() {}, nil)
	_ = function
}

func phis(parent context.Context, condition bool, key name) {
	var chosen any = key
	if condition {
		chosen = "text"
	}
	context.WithValue(parent, chosen, nil)
	text := "a"
	if condition {
		text = "b"
	}
	context.WithValue(parent, text, nil)
}

func callShapes(parent context.Context) {
	defer context.WithValue(parent, "deferred", nil)
	go context.WithValue(parent, []int{}, nil)
	func() {
		context.WithValue(parent, struct{}{}, nil)
	}()
	with := context.WithValue
	with(parent, "variable", nil)
	context.WithValue(
		parent,
		"split",
		nil,
	)
	if context.WithValue(parent, 3, nil) == nil {
		return
	}
	context.WithValue(context.WithValue(parent, "inner", nil), "outer", nil)
	_ = global
}

func generic[T any, C comparable, S ~string](parent context.Context, value T, key C, text S) {
	context.WithValue(parent, value, nil)
	context.WithValue(parent, key, nil)
	context.WithValue(parent, text, nil)
}

func instantiated(parent context.Context) {
	generic(parent, []int{}, "a", "b")
	generic[name, string, name](parent, "", "", "")
}
