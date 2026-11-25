package shadowed

// true and false are shadowed at package scope, each by an untyped boolean constant of the other's value, so both
// sides judge the constant and not its spelling: `for false {}` here loops forever and is reported, and
// `for true {}` never runs and is left alone. A package of its own, since the shadow holds for the whole package.

const (
	true  = 1 < 0
	false = 0 < 1
)

func reportedFalse() {
	for false {
	}
}

func leftAloneTrue() {
	for true {
	}
}
