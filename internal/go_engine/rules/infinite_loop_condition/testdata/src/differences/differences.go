package differences

import . "constants"

// The shapes where the port and the original could most plausibly part, and where both agree: a constant reached
// through a dot import, which is an identifier, constants made by iota, conversion and comparison, a parameter of a
// type constrained by ~bool, which is no constant, and loops nested in loops whose own conditions are constant.
// Nothing here is recorded, since the two differ nowhere.

const (
	first = iota == 0
	second
)

const converted = bool(true)

const typedFromUntyped bool = first

func reportedDotImport() {
	for On {
	}
}

func leftAloneDotImportFalse() {
	for Off {
	}
}

func reportedIota() {
	for first {
	}
}

func leftAloneIotaFalse() {
	for second {
	}
}

func reportedConverted() {
	for converted {
	}
	for typedFromUntyped {
	}
}

func reportedNestedInConstantLoops() {
	for true {
		for On {
			for {
				for first {
				}
			}
		}
	}
}

func leftAloneGenericParameter[Boolean ~bool](value Boolean) {
	for value {
	}
}

func reportedConstantInGeneric[Boolean ~bool](value Boolean) {
	for true {
		if value {
			return
		}
	}
}
