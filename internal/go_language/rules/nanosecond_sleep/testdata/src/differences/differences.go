// Package differences holds the shapes where the port's fixes could print otherwise than the original's: a literal
// on a line of its own, beside comments, inside parentheses, under signs and parentheses, and in a call spread over
// lines. The port and the original find and fix the same here, so the fixture records no difference.
package differences

import "time"

func shapes() {
	time.Sleep(
		1,
	)
	time.Sleep( /* before */ 2 /* after */)
	time.Sleep(3) // trailing
	time.Sleep((4))
	time.Sleep(-(+(-(5))))
	time.Sleep(
		// leading
		6,
	)
}
