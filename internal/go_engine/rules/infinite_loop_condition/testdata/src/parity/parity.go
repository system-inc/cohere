package parity

import "constants"

// Every shape S1006's logic branches on, and the near miss of each. A loop with neither an initializer nor a post
// statement whose condition is an identifier naming a constant of type bool, or an untyped boolean constant, whose
// value is true, is reported at its header; anything else is left alone.

const (
	untypedYes       = true
	untypedNo        = false
	typedYes    bool = true
	typedNo     bool = false
	computedYes      = !false && 1 < 2
	computedNo       = 1 > 2
)

// flag is a named boolean type, which IsBoolConst refuses, and alias an alias of bool, which it refuses as well.
type flag bool

type alias = bool

const (
	namedYes   flag  = true
	aliasedYes alias = true
)

func reportedTrue() {
	for true {
	}
}

func reportedUntypedConstant() {
	for untypedYes {
	}
}

func reportedTypedConstant() {
	for typedYes {
	}
}

func reportedComputedConstant() {
	for computedYes {
	}
}

func reportedLocalConstant() {
	const local = true
	for local {
	}
}

func reportedWithBody(values chan int) {
	for true {
		if <-values > 3 {
			break
		}
	}
}

func reportedLabeled() {
outer:
	for true {
		for true {
			break outer
		}
	}
}

func reportedInLiteral() func() {
	return func() {
		for true {
		}
	}
}

func reportedGeneric[Element any](values []Element) {
	for true {
		if len(values) > 0 {
			return
		}
	}
}

func reportedInsideOtherLoops(values []int) {
	for range values {
		for true {
			break
		}
	}
	for index := 0; index < 3; index++ {
		for untypedYes {
			break
		}
	}
}

func leftAloneFalse() {
	for false {
	}
	for untypedNo {
	}
	for typedNo {
	}
	for computedNo {
	}
}

func leftAloneNoCondition() {
	for {
	}
}

func leftAloneInitializer() {
	for index := 0; true; {
		index++
	}
}

func leftAlonePost(index int) {
	for ; true; index++ {
	}
}

func leftAloneBothClauses() {
	for index := 0; true; index++ {
	}
}

func leftAloneNamedAndAliased() {
	for namedYes {
	}
	for aliasedYes {
	}
}

func leftAloneExpressions(value int) {
	for !false {
	}
	for 1 == 1 {
	}
	for value == value {
	}
	for true && true {
	}
}

func leftAloneVariable() {
	running := true
	for running {
		running = false
	}
}

func leftAloneSelector() {
	for constants.On {
	}
	for constants.Off {
	}
}

func leftAloneShadowedTrue() {
	const true = false
	for true {
	}
}

func leftAloneShadowedTrueVariable() {
	true := 1 > 0
	for true {
	}
}
