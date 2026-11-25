// Package dotted calls time.Sleep through a dot import and a renamed import, which the fixes still spell time, as the
// original's do, and holds a local named time beside them.
package dotted

import (
	. "time"
	clock "time"
)

func dotted() {
	Sleep(1)
	clock.Sleep(2)
	clock.Sleep(200)
	time := 3
	clock.Sleep(Duration(time))
	Sleep(4)
}
