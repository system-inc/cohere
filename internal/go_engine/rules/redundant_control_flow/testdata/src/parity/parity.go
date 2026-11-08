package parity

import "fmt"

type number int

func (value number) method() {
	fmt.Println(value)
	return
}

func generic[Value any](value Value) {
	fmt.Println(value)
	return
}

var literal = func() {
	fmt.Println()
	return
}

func reportedBreaks(value int, boxed interface{}, values []int) {
	switch value {
	case 1:
		fmt.Println()
		break
	case 2, 3:
		fmt.Println()
		fmt.Println()
		break
	default:
		fmt.Println()
		break
	}

	// A switch with no tag has case clauses as one with a tag does.
	switch {
	case value > 0:
		value++
		break
	}

	// A type switch's clauses are case clauses too.
	switch typed := boxed.(type) {
	case int:
		fmt.Println(typed)
		break
	case string, nil:
		fmt.Println(typed)
		break
	}

	// Inside a loop, an unlabeled break still leaves the switch, not the loop, so it is still the default.
	for range values {
		switch value {
		case 1:
			fmt.Println()
			break
		}
	}

	// A case clause nested in another's body is judged on its own, and the outer one ends in the inner switch.
	switch value {
	case 1:
		fmt.Println()
		switch value {
		case 1:
			fmt.Println()
			break
		}
	}
}

func leftAloneBreaks(value int, channel chan int, values []int) {
	switch value {
	// A clause of a break alone is how a case says it does nothing.
	case 1:
		break
	// An empty clause has no last statement.
	case 2:
	// A break that is not last is no default.
	case 3:
		break
		fmt.Println()
	// The break inside an if is not the clause's last statement.
	case 4:
		fmt.Println()
		if value > 0 {
			break
		}
	// Other branches are not breaks.
	case 5:
		fmt.Println()
		fallthrough
	case 6:
		fmt.Println()
		goto end
	}
end:

	// A labeled break leaves an enclosing statement, not the switch.
outer:
	for range values {
		switch value {
		case 1:
			fmt.Println()
			break outer
		}
	}

	// The clause's last statement is a labeled statement, not the break it labels.
	switch value {
	case 1:
		fmt.Println()
		goto labeled
	labeled:
		break
	}

	// A select's clauses are comm clauses, which the check never judges.
	select {
	case received := <-channel:
		fmt.Println(received)
		break
	default:
		fmt.Println()
		break
	}

	// A for loop's break is no case clause's.
	for range values {
		fmt.Println()
		break
	}
}

func reportedReturn() {
	return
}

func reportedReturnWithParameters(value int, text string) {
	fmt.Println(value, text)
	return
}

func reportedReturnAfterUnreachable() {
	return
	fmt.Println()
	return
}

func reportedLiterals() {
	// Both the literal and the function it sits in end in a return.
	_ = func() {
		return
	}
	defer func(value int) {
		fmt.Println(value)
		return
	}(1)
	go func() {
		func() {
			return
		}()
		return
	}()
	return
}

func leftAloneResults() int {
	return 1
}

func leftAloneNamedResults() (count int) {
	count = 1
	return
}

func leftAloneUnnamedResults() (int, error) {
	return 0, nil
}

func leftAloneLiteralResults() {
	_ = func() int {
		return 1
	}
	_ = func() (text string) {
		return
	}
	// The literal's last statement is not a return, and the function's is an assignment.
	_ = func() {
		fmt.Println()
	}
}

// An empty body has no last statement.
func leftAloneEmpty() {}

func leftAloneConditional(value bool) {
	if value {
		return
	}
}

func leftAloneNotLast() {
	return
	fmt.Println()
}

func leftAloneLabeled() {
	goto last
last:
	return
}

// A declaration without a body is written elsewhere, in assembly, and has no statements to judge.
func leftAloneBodyless(value int)
