// Package stopping declares functions that never return, exported and not, and one that returns.
package stopping

import "stopping/under"

// Stop never returns.
func Stop() {
	panic("stop")
}

// halt never returns, since under.Abort does not, and is unexported, so no fact of it leaves the package.
func halt() {
	under.Abort()
}

// Goes returns whenever stopping is false.
func Goes(stopping bool) int {
	if stopping {
		halt()
	}
	return 1
}
