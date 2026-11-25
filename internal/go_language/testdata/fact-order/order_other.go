//go:build !windows

package order

// platformHook says so, and calls nothing of the package.
func platformHook() {
	println("hook")
}

// platformHold says so, and calls nothing of the package.
func platformHold() {
	println("hold")
}
