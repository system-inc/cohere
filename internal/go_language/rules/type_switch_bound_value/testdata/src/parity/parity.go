// Package parity holds every shape S1034's query and its judgment of each case branch on, the near miss of each, and
// every fix, for the original and the port to judge alike.
package parity

import (
	"fmt"
	"strconv"
)

type number int

type alias = int

type holder struct {
	value interface{}
}

var packageValue interface{}

func produce() interface{} { return nil }

func consume(values ...interface{}) {}

// One case asserts the switched variable to its own type, and the fix binds it.
func single(x interface{}) {
	switch x.(type) {
	case int:
		fmt.Println(x.(int))
	}
}

// Several cases, several assertions in one case, and an assertion inside another assertion's surroundings: every one
// is a related entry and every one is replaced.
func several(x interface{}) {
	switch x.(type) {
	case int:
		fmt.Println(x.(int) + x.(int))
	case string:
		fmt.Println(x.(string))
	case fmt.Stringer:
		fmt.Println(x.(fmt.Stringer).String())
	case *holder:
		consume(x.(*holder).value)
	}
}

// A switch that already binds asks nothing.
func alreadyBound(x interface{}) {
	switch y := x.(type) {
	case int:
		_ = y
		fmt.Println(x.(int))
	}
}

// An init statement keeps the query's nil Init from matching.
func withInit(x interface{}) {
	switch v := x; v.(type) {
	case int:
		fmt.Println(v.(int))
	}
}

// A parenthesized switched variable is bound inside its parentheses, and the fix keeps them in what it assigns.
func parenthesizedSwitched(x interface{}) {
	switch (x).(type) {
	case int:
		fmt.Println(x.(int))
	}
}

// A parenthesized assertion in a case is one the original reads as unrelated, so that case is left alone and the
// switch takes no fix, while the other case is still found.
func parenthesizedOffender(x interface{}) {
	switch x.(type) {
	case int:
		fmt.Println((x).(int))
	case string:
		fmt.Println(x.(string))
	}
}

// An assertion of another variable in one case leaves that case alone and withholds the fix.
func unrelatedVariable(x interface{}, y interface{}) {
	switch x.(type) {
	case int:
		fmt.Println(x.(int), y.(int))
	case string:
		fmt.Println(x.(string))
	}
}

// An assertion to another type in one case leaves that case alone and withholds the fix.
func unrelatedType(x interface{}) {
	switch x.(type) {
	case int:
		_, isString := x.(string)
		fmt.Println(isString)
	case bool:
		fmt.Println(x.(bool))
	}
}

// Only unrelated assertions: nothing is found.
func onlyUnrelated(x interface{}, y interface{}) {
	switch x.(type) {
	case int:
		fmt.Println(y.(int))
	case string:
		fmt.Println(x.(int))
	}
}

// A case of several types and the default are not judged, and do not withhold the fix.
func severalTypesAndDefault(x interface{}) {
	switch x.(type) {
	case int, string:
		fmt.Println(x.(int))
	case bool:
		fmt.Println(x.(bool))
	default:
		fmt.Println(x.(float64))
	}
}

// A case of several types and the default alone find nothing.
func severalTypesAlone(x interface{}) {
	switch x.(type) {
	case int, string:
		fmt.Println(x.(int))
	default:
		fmt.Println(x.(int))
	}
}

// An alias is its type, and a defined type is not its underlying one.
func aliasAndDefined(x interface{}) {
	switch x.(type) {
	case alias:
		fmt.Println(x.(int))
	}
	switch x.(type) {
	case number:
		fmt.Println(x.(int))
	}
	switch x.(type) {
	case number:
		fmt.Println(x.(number))
	}
}

// A variable that shadows the switched one in a case is another variable.
func shadowed(x interface{}, y interface{}) {
	switch x.(type) {
	case int:
		x := y
		fmt.Println(x.(int))
	}
}

// An assertion in a function literal of the case is one of its own.
func inFunctionLiteral(x interface{}) {
	switch x.(type) {
	case int:
		func() {
			fmt.Println(x.(int))
		}()
	}
}

// A nested type switch on the same variable asserts it to no type, so the outer case is left alone, and the inner
// switch is judged as its own.
func nestedSameVariable(x interface{}) {
	switch x.(type) {
	case int:
		switch x.(type) {
		case int:
			fmt.Println(x.(int))
		}
	}
}

// A nested type switch on another variable is an unrelated assertion of the outer case.
func nestedOtherVariable(x interface{}, y interface{}) {
	switch x.(type) {
	case int:
		switch y.(type) {
		case string:
			fmt.Println(y.(string), x.(int))
		}
	case bool:
		fmt.Println(x.(bool))
	}
}

// A nil case asserts to no type an assertion could name.
func nilCase(x interface{}) {
	switch x.(type) {
	case nil:
		fmt.Println(x.(int))
	}
}

// A switched selector, call or index is no identifier.
func notIdentifiers(value holder, values []interface{}) {
	switch value.value.(type) {
	case int:
		fmt.Println(value.value.(int))
	}
	switch produce().(type) {
	case int:
		fmt.Println(produce().(int))
	}
	switch values[0].(type) {
	case int:
		fmt.Println(values[0].(int))
	}
}

// A package's variable is a variable like any other.
func packageVariable() {
	switch packageValue.(type) {
	case string:
		fmt.Println(packageValue.(string))
	}
}

// A labeled switch is a type switch the inspector hands over alike.
func labeled(x interface{}) {
outer:
	switch x.(type) {
	case int:
		if x.(int) > 0 {
			break outer
		}
	}
}

// A type parameter is a case's type like any other.
func generic[T any](x interface{}) {
	switch x.(type) {
	case T:
		consume(x.(T))
	}
}

// A comment in the guard is no part of what the fix prints.
func commented(x interface{}) {
	switch x. /* the value */ (type) {
	case int:
		fmt.Println(strconv.Itoa(x.(int)))
	}
}

// A case with no assertion and an empty switch find nothing.
func nothing(x interface{}) {
	switch x.(type) {
	case int:
	}
	switch x.(type) {
	}
}
