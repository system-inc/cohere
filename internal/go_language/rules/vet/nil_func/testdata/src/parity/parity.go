// Package parity holds every branch of nilfunc's judgment, each with a clean case beside it, and the shapes most
// likely to set the port and x/tools' nilfunc apart: generics, aliases, method values and expressions, closures,
// embedded fields, qualified and dot imports. The differential test runs both over each and compares.
package parity

import (
	. "path"
	"strings"
)

func declared() {}

func generic[T any](value T) T { return value }

func pair[First any, Second any](first First, second Second) {}

type value struct {
	callback func()
}

func (value) method() {}

func (*value) pointerMethod() {}

type wrapper struct {
	value
	*strings.Builder
	handler func()
}

type box[T any] struct {
	held     T
	callback func() T
}

func (box[T]) get() T {
	var zero T
	return zero
}

type caller interface {
	call()
}

type alias = value

type boxAlias = box[int]

type functionType func()

var declaredValue = declared

var packageLevel = declared == nil

var packageLevelVariable = declaredValue == nil

func returnsFunction() func() { return declared }

// Each operator on each side, and the other operators, which the pass leaves alone.
func operators() {
	_ = declared == nil
	_ = declared != nil
	_ = nil == declared
	_ = nil != declared
	_ = declared == nil && declared != nil
	_ = (declared == nil) == true
	_ = !(declared != nil)
}

// Parentheses around the function, around nil, and around both.
func parentheses() {
	_ = (declared) == nil
	_ = declared == (nil)
	_ = (declared) != (nil)
	_ = (nil) == (declared)
}

// Variables, fields, parameters, results and literals of function type may be nil, or are not names, and are left
// alone.
func notFunctions(parameter func(), values []func(), table map[string]func()) {
	var local func()
	var instance value
	var named functionType
	_ = local == nil
	_ = parameter != nil
	_ = instance.callback == nil
	_ = declaredValue == nil
	_ = named == nil
	_ = values[0] == nil
	_ = table["key"] == nil
	_ = returnsFunction() == nil
	_ = func() {} == nil
	_ = (func())(declared) == nil
	_ = functionType(declared) != nil
}

// Method values on a value, a pointer and an alias, and method expressions on the type and its pointer.
func methods(pointer *value, through caller) {
	var instance value
	var aliased alias
	_ = instance.method == nil
	_ = instance.pointerMethod != nil
	_ = pointer.method == nil
	_ = pointer.pointerMethod == nil
	_ = aliased.method != nil
	_ = value.method == nil
	_ = (*value).pointerMethod == nil
	_ = alias.method == nil
	_ = through.call == nil
	_ = caller.call != nil
}

// Methods and fields promoted through embedding, by value and by pointer.
func embedded(outer wrapper, pointer *wrapper) {
	_ = outer.method == nil
	_ = outer.value.method == nil
	_ = outer.pointerMethod != nil
	_ = outer.callback == nil
	_ = outer.handler == nil
	_ = outer.WriteString == nil
	_ = pointer.Builder.Len != nil
	_ = pointer.callback != nil
}

// Instantiations explicit in one and two type arguments, a generic type's method values and expressions, and its
// fields of function type, which are left alone.
func generics[T any](held box[T], aliased boxAlias) {
	_ = generic[int] == nil
	_ = generic[T] != nil
	_ = (generic[string]) == nil
	_ = pair[int, T] == nil
	_ = (pair[T, T]) != nil
	_ = held.get == nil
	_ = aliased.get != nil
	_ = box[T].get == nil
	_ = boxAlias.get == nil
	_ = held.callback == nil
	_ = aliased.callback != nil
	var instantiated func(int) int = generic[int]
	_ = instantiated == nil
	functions := []func(T) T{generic[T]}
	_ = functions[0] == nil
}

// A package's function named through its import, and one dot imported.
func imports() {
	_ = strings.ToUpper == nil
	_ = strings.ToUpper != nil
	_ = Base == nil
	_ = (Join) != nil
	var builder strings.Builder
	_ = builder.String == nil
}

// Comparisons in closures, in a deferred and a spawned literal, and in a closure inside a generic function.
func closures() {
	check := func() bool {
		return declared == nil
	}
	_ = check
	defer func() {
		_ = declared != nil
	}()
	go func() {
		_ = generic[int] == nil
	}()
}

func genericClosure[T any]() func() bool {
	return func() bool {
		return generic[T] != nil
	}
}

// A local that shadows a function's name is a variable, and is left alone. (A function compared with anything but
// the predeclared nil does not type-check, so neither a typed nil nor a shadowed nil can stand beside one.)
func shadows() {
	declared := returnsFunction()
	_ = declared == nil
}

// Comparisons in other statements: conditions, switches, returns and assignments.
func statements() bool {
	if declared == nil {
		return false
	}
	for declared != nil {
		break
	}
	switch {
	case declared == nil:
		return true
	}
	// A switch on the function itself is not a binary expression, and both passes leave it alone.
	switch declared {
	case nil:
		return true
	}
	result := strings.ToLower != nil
	return result
}
