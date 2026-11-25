// Package parity holds the defers SA9010 judges, flagged and left alone, for the port and the original to judge side
// by side: calls returning a function through every route a function is reached by, named function types, generic
// results, builtins, and calls returning nothing, a value or several.
package parity

type callback func()

type maker func() callback

type holder struct {
	make maker
}

func returnsFunction() func()                        { return func() {} }
func returnsCallback() callback                      { return func() {} }
func returnsGeneric[Result any](value Result) Result { return value }
func returnsTwo() (func(), func())                   { return func() {}, func() {} }
func returnsNothing()                                {}

func (holder) method() func(int) bool { return func(int) bool { return true } }

func flagged(build maker, value holder, pointer *holder, table map[string]func() func(), list []func() func()) {
	defer returnsFunction()
	defer returnsCallback()
	defer build()
	defer value.make()
	defer value.method()
	defer pointer.method()
	defer table["key"]()
	defer list[0]()
	defer returnsGeneric(returnsFunction)
	defer returnsGeneric[func() func()](returnsFunction)
	defer (returnsFunction)()
	defer holder.method(value)
	go func() {
		defer returnsFunction()
	}()
}

func spared(build maker, value holder, list []func() func()) {
	defer returnsFunction()()
	defer build()()
	defer value.method()(1)
	defer list[0]()()
	defer returnsTwo()
	defer returnsNothing()
	defer returnsGeneric(1)
	defer println()
	defer close(make(chan int))
	defer recover()
	defer func() {}()
	go returnsFunction()
}
