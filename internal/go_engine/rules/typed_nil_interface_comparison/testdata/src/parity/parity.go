// Package parity holds the comparisons with nil SA4023 judges, flagged and left alone, for the port and the original
// to judge side by side: results of functions in this package and another, by every position up to the twelfth,
// methods, function values, closures and interface methods, interface values made from concrete values, parameters,
// constants, zero values, comma-ok forms, range variables and named results, == and !=, both sides swapped, switches,
// phis, generic code and type parameters.
package parity

import (
	"errors"
	"fmt"

	"parity/producer"
)

type failure struct{}

func (*failure) Error() string { return "failure" }

func typedNil() error {
	println("not trivial")
	var value *failure
	return value
}

func trivialTypedNil() error {
	return (*failure)(nil)
}

func neverNilHoldingValue() error {
	println("not trivial")
	return errors.New("value")
}

func sometimesUntyped(choose bool) error {
	println("not trivial")
	if choose {
		return nil
	}
	var value *failure
	return value
}

func pair() (int, error) {
	println("not trivial")
	var value *failure
	return 0, value
}

func triple() (int, int, error) {
	println("not trivial")
	var value *failure
	return 0, 0, value
}

func quadruple() (int, int, int, error) {
	println("not trivial")
	var value *failure
	return 0, 0, 0, value
}

type receiver struct{}

func (receiver) typed() error {
	println("not trivial")
	var value *failure
	return value
}

func (*receiver) pointerTyped() error {
	println("not trivial")
	var value *failure
	return value
}

func results(holder producer.Holder, method interface{ Get() error }) {
	_ = typedNil() == nil
	_ = typedNil() != nil
	_ = trivialTypedNil() == nil
	_ = neverNilHoldingValue() == nil
	_ = sometimesUntyped(true) == nil
	_, second := pair()
	_ = second == nil
	_, _, third := triple()
	_ = third != nil
	_, _, _, fourth := quadruple()
	_ = fourth == nil
	_, _, _, _, _, _, _, _, _, _, _, twelfth := producer.Many()
	_ = twelfth == nil
	_ = producer.TypedNil() == nil
	_ = producer.Untyped() == nil
	_ = holder.Get() == nil
	_ = method.Get() == nil
	_ = receiver{}.typed() == nil
	_ = (&receiver{}).pointerTyped() != nil
	value := typedNil
	_ = value() == nil
	_ = func() error { println("not trivial"); return (*failure)(nil) }() == nil
	_ = nil == typedNil()
	_ = (typedNil()) == (nil)
	if err := typedNil(); err != nil {
		fmt.Println(err)
	}
	switch typedNil() {
	case nil:
	}
	func() {
		_ = producer.TypedNil() == nil
	}()
}

func merged(choose bool) {
	var err error
	if choose {
		err = typedNil()
	} else {
		err = producer.TypedNil()
	}
	_ = err == nil
	err = typedNil()
	if choose {
		err = typedNil()
	}
	_ = err != nil
}

func made(pointer *int, number int, err error) {
	var fromPointer interface{} = pointer
	_ = fromPointer == nil
	var fromNumber interface{} = number
	_ = fromNumber != nil
	var fromConstant interface{} = 1
	_ = fromConstant == nil
	var fromNil interface{} = (*int)(nil)
	_ = fromNil == nil
	var fromError interface{} = err
	_ = fromError == nil
	_ = interface{}(pointer) == nil
	_ = error((*failure)(nil)) != nil
}

func zeroValues(choose bool) {
	var number int
	var fromZero interface{} = number
	_ = fromZero == nil
	var pointer *int
	var fromZeroPointer interface{} = pointer
	_ = fromZeroPointer != nil
	var chosen int
	if choose {
		chosen = 1
	}
	var fromChosen interface{} = chosen
	_ = fromChosen == nil
}

func madeFromOtherValues(table map[string]int, boxed interface{}, channel chan int, values []int) (named int) {
	looked, _ := table["key"]
	var fromLookup interface{} = looked
	_ = fromLookup == nil
	asserted, _ := boxed.(int)
	var fromAssertion interface{} = asserted
	_ = fromAssertion == nil
	received, _ := <-channel
	var fromReceive interface{} = received
	_ = fromReceive == nil
	for index, value := range values {
		var fromIndex interface{} = index
		_ = fromIndex == nil
		var fromValue interface{} = value
		_ = fromValue == nil
	}
	var fromNamed interface{} = named
	_ = fromNamed == nil
	var fromLength interface{} = len(values)
	_ = fromLength == nil
	var fromString interface{} = "a" + "b"
	_ = fromString == nil
	return named
}

func genericAny[Value any](value Value) {
	_ = any(value) == nil
}

func genericConcrete[Value ~int | ~string](value Value) {
	_ = any(value) == nil
}

func genericPointer[Value ~*int](value Value) {
	_ = any(value) != nil
}

func genericError[Value error](value Value) {
	_ = error(value) == nil
}

func genericNilable[Value *int | *string](value Value) {
	_ = value == nil
	_ = any(value) == nil
}

func genericResult[Value any](value Value) any {
	_ = value
	return any(value)
}

func generic() {
	_ = genericResult[error](nil) == nil
	_ = genericResult[*int](nil) == nil
}
