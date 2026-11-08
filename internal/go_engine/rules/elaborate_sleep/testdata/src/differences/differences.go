// Package differences holds where the port's fix parts from the original's, which writes time.Sleep whatever the file
// calls the package time: here a file that imports it under another name alone. Its two packages beside it hold a
// time that names a variable, and a dot import. Each record says what the port does, its reason on the line above.
package differences

import clock "time"

// renamed calls time.After under the file's one name for time.
func renamed() {
	// The file names time clock alone, so the original's time.Sleep(clock.Second) names nothing here and does not
	// compile, and the port writes clock.Sleep(clock.Second).
	select { // port moves: should use time.Sleep instead of elaborate way of sleeping
	case <-clock.After(clock.Second):
	}
	// A body that is not empty takes no fix, so the two agree.
	select {
	case <-clock.After(clock.Second):
		println("waited")
	}
}
