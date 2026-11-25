package flagged

import "fmt"

func start() {}

func stop() {}

func apply(transform func(int) int) int { return transform(1) }

// qualityMode is the first site's shape in Go: a quality flag that picks the same mode either way.
func qualityMode(highQuality bool) string {
	mode := ""
	if highQuality { // want "chooses nothing"
		mode = "pro"
	} else {
		mode = "pro"
	}
	return mode
}

// commentsAndLayout differ only in what the comparison ignores.
func commentsAndLayout(ready bool) {
	if ready { // want "chooses nothing"
		// The fast path.
		start()
	} else {

		/* The slow path. */
		start()
	}
}

// wholeChain ignores both of its conditions, so it is one finding on the whole chain.
func wholeChain(first bool, second bool) {
	if first { // want "chooses nothing"
		start()
	} else if second {
		start()
	} else {
		start()
	}
}

// tailOfChain ignores only its second condition, so the finding is on that if.
func tailOfChain(first bool, second bool) {
	if first {
		stop()
	} else if second { // want "chooses nothing"
		start()
	} else {
		start()
	}
}

// bareExits ignore their condition exactly as any other pair of matching branches does.
func bareExits(done bool) {
	if done { // want "chooses nothing"
		return
	} else {
		return
	}
}

// panics are reported too: two copies of a crash still choose nothing.
func panics(broken bool) {
	if broken { // want "chooses nothing"
		panic("unreachable state")
	} else {
		panic("unreachable state")
	}
}

// nestedAgainstElseIf is the twins' rare form: the first branch's only statement is the else-if whole.
func nestedAgainstElseIf(outer bool, inner bool) {
	if outer { // want "chooses nothing"
		if inner {
			start()
		} else {
			stop()
		}
	} else if inner {
		start()
	} else {
		stop()
	}
}

// initVariable is one object in both branches, since the init statement's scope holds the whole if.
func initVariable(parse func() (int, error)) {
	if count, parseError := parse(); parseError != nil { // want "chooses nothing"
		fmt.Println(count)
	} else {
		fmt.Println(count)
	}
}

// ownDeclarations are two objects, each used on its own side, which is the same code.
func ownDeclarations(verbose bool) {
	if verbose { // want "chooses nothing"
		total := 1
		label := "café ☕"
		fmt.Println(total, label)
	} else {
		total := 1
		label := "café ☕"
		fmt.Println(total, label)
	}
}

// functionLiterals declare their parameters on each side.
func functionLiterals(double bool) int {
	if double { // want "chooses nothing"
		return apply(func(value int) int { return value * 2 })
	} else {
		return apply(func(value int) int { return value * 2 })
	}
}

// localTypes declare their fields on each side, and each side reads its own.
func localTypes(wide bool) int {
	if wide { // want "chooses nothing"
		type size struct{ width int }
		return size{width: 4}.width
	} else {
		type size struct{ width int }
		return size{width: 4}.width
	}
}

// nestedTypeSwitches bind a variable in clauses that match each other, so each side's variable is the same name.
func nestedTypeSwitches(careful bool, value any) {
	if careful { // want "chooses nothing"
		switch typed := value.(type) {
		case int:
			fmt.Println(typed + 1)
		}
	} else {
		switch typed := value.(type) {
		case int:
			fmt.Println(typed + 1)
		}
	}
}

// zeroValue is generic, and both branches name the same type parameter.
func zeroValue[Element any](empty bool) Element {
	if empty { // want "chooses nothing"
		var zero Element
		return zero
	} else {
		var zero Element
		return zero
	}
}

// shadowedInits widen only as far as the names agree: the outer condition still picks which total is printed.
func shadowedInits(first bool, second bool) {
	if total := 1; first {
		fmt.Println(total)
	} else if total := 2; second { // want "chooses nothing"
		fmt.Println(total)
	} else {
		fmt.Println(total)
	}
}

// expressionSwitch runs the same clause for every mode.
func expressionSwitch(mode string) {
	switch mode { // want "chooses nothing"
	case "fast":
		start()
	case "slow":
		start()
	default:
		start()
	}
}

// taglessSwitch has its default first, which changes nothing about what it chooses.
func taglessSwitch(count int) string {
	switch { // want "chooses nothing"
	default:
		return "some"
	case count > 10:
		return "some"
	}
}

// typeSwitchWithoutBinding asks the dynamic type and does the same with every answer.
func typeSwitchWithoutBinding(value any) {
	switch value.(type) { // want "chooses nothing"
	case int, int64:
		fmt.Println(value)
	default:
		fmt.Println(value)
	}
}

// insideSwitch is judged on its own branches, wherever it sits.
func insideSwitch(mode string, ready bool) {
	switch mode {
	case "fast":
		if ready { // want "chooses nothing"
			start()
		} else {
			start()
		}
	default:
		stop()
	}
}
