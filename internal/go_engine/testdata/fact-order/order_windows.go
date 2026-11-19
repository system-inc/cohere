package order

// lastOrder holds what platformHook last read of orderA.
var lastOrder error

// platformHook is windows's, which calls orderA back, closing the cycle orderA, orderB, platformHook.
func platformHook() {
	println("hook")
	lastOrder = orderA()
}
