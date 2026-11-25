// Package parity holds every branch of unreachable's walk, each terminating statement and each statement found dead,
// clean cases beside them, and the shapes most likely to differ: generics, methods, closures, nested function
// literals, and every way a name `panic` can mean something other than the builtin. Every file type-checks, and every
// dead statement stands on lines of its own; the layouts that share a line are in testdata/layout.txtar.
package parity

import "fmt"

var channel chan int
var condition bool
var value any
var number int

// The terminating statements, each followed by a dead one. A statement after a fallthrough does not type-check, so
// fallthrough ends a case below instead.

func afterReturn() {
	return
	fmt.Println("dead") // want "unreachable code"
}

func afterPanic() {
	panic("stop")
	fmt.Println("dead") // want "unreachable code"
}

func afterGoto() {
	goto end
	fmt.Println("dead") // want "unreachable code"
end:
	fmt.Println("reached by the goto")
}

func afterBreak() {
	for {
		break
		fmt.Println("dead") // want "unreachable code"
	}
}

func afterContinue() {
	for index := 0; index < 3; index++ {
		continue
		fmt.Println("dead") // want "unreachable code"
	}
}

func afterLoopWithoutCondition() {
	for {
	}
	fmt.Println("dead") // want "unreachable code"
}

func afterEmptySelect() {
	select {}
	fmt.Println("dead") // want "unreachable code"
}

// Only the first of a run of dead statements is reported, and the walk goes on after it.

func onlyTheFirstOfARun() {
	return
	fmt.Println("dead") // want "unreachable code"
	fmt.Println("dead too, unreported")
	return
	fmt.Println("dead again") // want "unreachable code"
}

// Every kind of statement, found dead.

func deadStatements(values []int) {
	return
	number = 1 // want "unreachable code"
	return
	var declared = 2 // want "unreachable code"
	_ = declared
	return
	defer fmt.Println() // want "unreachable code"
	return
	go fmt.Println() // want "unreachable code"
	return
	number++ // want "unreachable code"
	return
	channel <- 1 // want "unreachable code"
	return
	{ // want "unreachable code"
		fmt.Println()
	}
	return
	if condition { // want "unreachable code"
		fmt.Println()
	}
	return
	for condition { // want "unreachable code"
	}
	return
	for range values { // want "unreachable code"
	}
	return
	select { // want "unreachable code"
	case <-channel:
	default:
	}
	return
	switch number { // want "unreachable code"
	case 1:
	}
	return
	switch value.(type) { // want "unreachable code"
	case int:
	}
	return
	panic("dead") // want "unreachable code"
	return        // want "unreachable code"
}

func deadBranches() {
	for {
		return
		break // want "unreachable code"
	}
	for {
		return
		continue // want "unreachable code"
	}
}

func deadLabeled() {
	return
labeled: // want "unreachable code"
	for {
		break labeled
	}
}

func deadGotoAndFallthrough() {
	switch number {
	case 1:
		return
		fallthrough // want "unreachable code"
	case 2:
		return
		goto end // want "unreachable code"
	}
end:
}

// A dead statement on several lines is deleted with all of them, and its comments too.

func deadOverLines() {
	return
	// A comment on a line of its own above the dead statement stays.
	if condition { // want "unreachable code"
		fmt.Println("the whole if goes")
	} else {
		fmt.Println("and its else")
	} // a comment after it goes with it
}

// A labeled statement some goto names is reachable by it.

func gotoTarget() {
	goto target
	return // want "unreachable code"
target:
	fmt.Println("reached")
}

func gotoTargetBehind() {
target:
	if condition {
		goto target
	}
	return
target2:
	fmt.Println("reached by the goto below")
	goto target2
}

func gotoItself() {
	return
loop:
	goto loop
}

// A loop with a condition, or with a break, falls through.

func loops(values []int) {
	for condition {
	}
	fmt.Println("reached")
	for {
		if condition {
			break
		}
	}
	fmt.Println("reached")
outer:
	for {
		for {
			break outer
		}
	}
	fmt.Println("reached")
	for range values {
		return
	}
	fmt.Println("reached")
	for index := 0; ; index++ {
		if index > 3 {
			break
		}
	}
	fmt.Println("reached")
}

func loopWithInnerBreakOnly() {
	for {
		for {
			break
		}
	}
	fmt.Println("dead") // want "unreachable code"
}

func loopWithContinueOuter() {
outer:
	for {
		for {
			continue outer
		}
	}
	fmt.Println("dead") // want "unreachable code"
}

// A break inside a select or switch leaves it, not the loop around it.

func breakLeavesTheSelect() {
	for {
		select {
		case <-channel:
			break
		}
	}
	fmt.Println("dead") // want "unreachable code"
}

func breakLeavesTheSwitch() {
	for {
		switch number {
		case 1:
			break
		}
	}
	fmt.Println("dead") // want "unreachable code"
}

func breakLeavesTheTypeSwitch() {
	for {
		switch value.(type) {
		default:
			break
		}
	}
	fmt.Println("dead") // want "unreachable code"
}

func labeledBreakLeavesTheLoop() {
loop:
	for {
		select {
		case <-channel:
			break loop
		}
	}
	fmt.Println("reached")
}

// A break in a function literal leaves nothing of the function around it.

func breakInALiteral() {
	for {
		func() {
			for {
				break
			}
		}()
	}
	fmt.Println("dead") // want "unreachable code"
}

// A select falls through when some case does, or a break leaves it, whether or not it has a default.

func selects() {
	select {
	case <-channel:
		return
	case channel <- 1:
		panic("stop")
	}
	fmt.Println("dead") // want "unreachable code"
}

func selectsFallingThrough() {
	select {
	case <-channel:
		return
	case channel <- 1:
	}
	fmt.Println("reached")
	select {
	case <-channel:
		return
	default:
		break
	}
	fmt.Println("reached")
	select {
	case received := <-channel:
		fmt.Println(received)
		return
		fmt.Println("dead") // want "unreachable code"
	}
}

// A switch falls through when it has no default, some case does, or a break leaves it.

func switches() {
	switch {
	case condition:
		return
	default:
		panic("stop")
	}
	fmt.Println("dead") // want "unreachable code"
}

func switchesFallingThrough() {
	switch {
	case condition:
		return
	}
	fmt.Println("reached")
	switch {
	case condition:
		return
	default:
	}
	fmt.Println("reached")
labeled:
	switch {
	case condition:
		for {
			break labeled
		}
	default:
		return
	}
	fmt.Println("reached")
	switch initialized := number; initialized {
	case 1:
		fallthrough
	default:
		return
	}
	fmt.Println("dead") // want "unreachable code"
}

func typeSwitches() {
	switch typed := value.(type) {
	case int:
		fmt.Println(typed)
		return
	default:
		panic(typed)
	}
	fmt.Println("dead") // want "unreachable code"
}

func typeSwitchesFallingThrough() {
	switch value.(type) {
	case int:
		return
	}
	fmt.Println("reached")
	switch value.(type) {
	case int:
		return
	default:
		break
	}
	fmt.Println("reached")
}

// An if falls through unless it has an else and both ways end.

func ifs() {
	if condition {
		return
	} else if number > 1 {
		panic("stop")
	} else {
		for {
		}
	}
	fmt.Println("dead") // want "unreachable code"
}

func ifsFallingThrough() {
	if condition {
		return
	}
	fmt.Println("reached")
	if condition {
		return
	} else if number > 1 {
		panic("stop")
	}
	fmt.Println("reached")
	if condition {
	} else {
		return
	}
	fmt.Println("reached")
}

// Only the builtin panic ends the flow: a name the parser resolves to a declaration in the file is something else.

func panicVariable() {
	var panic = func(any) {}
	panic("not the builtin")
	fmt.Println("reached")
}

func panicParameter(panic func(any)) {
	panic("not the builtin")
	fmt.Println("reached")
}

func panicResult() (panic func(any)) {
	panic = func(any) {}
	panic("not the builtin")
	fmt.Println("reached")
	return panic
}

func panicCaptured() {
	panic := func(any) {}
	func() {
		panic("not the builtin")
		fmt.Println("reached")
	}()
}

func panicShadowedInABlock() {
	{
		panic := func(any) {}
		panic("not the builtin")
		fmt.Println("reached")
	}
	panic("the builtin again")
	fmt.Println("dead") // want "unreachable code"
}

type panicker struct{}

func (panicker) panic(any) {}

func panicMethod(receiver panicker) {
	receiver.panic("a method, not the builtin")
	fmt.Println("reached")
}

func panicParenthesized() {
	(panic)("the builtin, but not a bare name, so not judged")
	fmt.Println("reached")
}

func panicDeferred() {
	defer panic("deferred")
	fmt.Println("reached")
}

// Generics, methods and function literals each have their own walk.

func generic[Value any](given Value) Value {
	return given
	fmt.Println("dead") // want "unreachable code"
	return given
}

type box[Value any] struct{ held Value }

func (holder box[Value]) get() Value {
	return holder.held
	fmt.Println("dead") // want "unreachable code"
	return holder.held
}

func (holder *box[Value]) set(given Value) {
	holder.held = given
	return
	holder.held = given // want "unreachable code"
}

var literal = func() {
	return
	fmt.Println("dead") // want "unreachable code"
}

func literals() {
	inner := func() {
		panic("stop")
		fmt.Println("dead") // want "unreachable code"
	}
	inner()
	return
	go func() { // want "unreachable code"
		return
		fmt.Println("dead in the literal, which has a walk of its own") // want "unreachable code"
	}()
}

// An unreachable statement inside a case or communication clause.

func clauses() {
	switch number {
	case 1, 2:
		return
		fmt.Println("dead") // want "unreachable code"
	}
	select {
	case <-channel:
		return
		fmt.Println("dead") // want "unreachable code"
	default:
		panic("stop")
		fmt.Println("dead") // want "unreachable code"
	}
}
