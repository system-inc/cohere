package allowed

import "fmt"

func start() {}

func stop() {}

// qualityMode is the first site after its repair: the flag picks between two modes.
func qualityMode(highQuality bool) string {
	mode := "std"
	if highQuality {
		mode = "pro"
	} else {
		mode = "std"
	}
	return mode
}

// noElse has one branch, so there is nothing to compare it with.
func noElse(ready bool) {
	if ready {
		start()
	}
}

// longOr is `if first || second` written long: both conditions still decide between start and stop.
func longOr(first bool, second bool) {
	if first {
		start()
	} else if second {
		start()
	} else {
		stop()
	}
}

// noFinalElse runs nothing when no condition holds, so every condition decides something.
func noFinalElse(first bool, second bool) {
	if first {
		start()
	} else if second {
		start()
	}
}

// emptyBranches run nothing, which is a different smell, and a commented one says what its author meant.
func emptyBranches(ready bool, warm bool) {
	if ready {
	} else {
	}
	if warm {
		// The cache is already warm.
	} else {
		// Nothing to warm yet.
	}
}

// oneEmpty does something on one side only.
func oneEmpty(ready bool) {
	if ready {
		start()
	} else {
	}
}

// switchWithoutDefault runs nothing for any other mode, so its cases still decide.
func switchWithoutDefault(mode string) {
	switch mode {
	case "fast":
		start()
	case "slow":
		start()
	}
}

// switchWithDifferentDefault is `case "fast", "slow"` written long.
func switchWithDifferentDefault(mode string) {
	switch mode {
	case "fast":
		start()
	case "slow":
		start()
	default:
		stop()
	}
}

// loneDefault has one clause, so there is nothing to compare it with.
func loneDefault(mode string) {
	switch mode {
	default:
		start()
	}
}

// emptyClauses run nothing for any mode.
func emptyClauses(mode string) {
	switch mode {
	case "fast":
	default:
	}
}

// differentClauses choose between two things.
func differentClauses(value any) {
	switch value.(type) {
	case int:
		fmt.Println("number")
	default:
		fmt.Println("other")
	}
}
