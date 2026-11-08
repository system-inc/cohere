// Package drop_misplaced records a drop on the line above the call to lost, which only the original finds, so the
// record covers nothing and the finding is a regression.
package drop_misplaced

func lost() {}

func calls() {
	_ = 0 // port drops: call to lost
	lost()
}
