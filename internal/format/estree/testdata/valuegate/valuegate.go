// Package valuegate is TestTheGateFindsEveryPlantedConversion's fixture: a sealed Value of its own, the shape
// estree's has, converted to an empty interface once in each way Go converts one implicitly. Each line that
// converts is marked planted, and the gate must find exactly those lines: the others are the uses it must
// let pass, a Value kept a Value, a value that is not one, and fmt's and testing's printing.
package valuegate

import (
	"fmt"
	"testing"
)

type Value interface{ isValue() }

type String string

type Node struct{}

func (String) isValue() {}

func (*Node) isValue() {}

// Anything is an empty interface under another name, which is still one.
type Anything interface{}

type holder struct {
	anything any
	value    Value
}

func takesAny(value any) {}

func takesAnything(values ...any) {}

func takesValue(value Value) {}

func takesTwo(value any, other any) {}

func pair() (Value, String) { return String("a"), String("b") }

func returnsAny(value Value) any {
	return value // planted
}

func returnsTwo() (any, any) {
	return pair() // planted
}

func Plant(test *testing.T, value Value, text String, node *Node, channel chan any, table map[any]bool) {
	takesAny(value)                           // planted
	takesAnything(text)                       // planted
	takesAnything(String("x"), "not a value") // planted
	takesTwo(pair())                          // planted
	takesAny(any(node))                       // planted
	_ = Anything(text)                        // planted
	_ = append([]any{}, node)                 // planted
	var stored any = value                    // planted
	stored = text                             // planted
	var first, second any
	first, second = pair()          // planted
	_ = []any{node}                 // planted
	_ = map[string]any{"key": text} // planted
	_ = map[any]bool{value: true}   // planted
	_ = holder{anything: value}     // planted
	_ = holder{text, nil}           // planted
	channel <- node                 // planted
	_ = table[text]                 // planted
	_ = stored == value             // planted
	_ = func() any { return node }  // planted

	takesValue(text)
	takesAny("not a value")
	takesAny(Node{})
	var kept Value = text
	_ = holder{value: node}
	_ = []Value{text, node}
	_ = kept == value
	_ = value == nil
	fmt.Println(value, text, node)
	_ = fmt.Sprintf("%v", value)
	_ = fmt.Errorf("%v", text)
	test.Logf("%v %v", value, node)
	switch typed := value.(type) {
	case String:
		takesValue(typed)
	}
	_, _, _ = stored, first, second
}
