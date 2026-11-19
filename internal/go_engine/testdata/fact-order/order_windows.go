package order

// lastOrder holds what platformHook last read of orderA.
var lastOrder error

// platformHook is windows's, which calls orderA back, closing the cycle orderA, orderB, platformHook.
func platformHook() {
	println("hook")
	lastOrder = orderA()
}

// platformHold is windows's, which calls orderC back, closing the cycle orderC, box.hold, platformHold.
func platformHold() {
	println("hold")
	orderC()
}
