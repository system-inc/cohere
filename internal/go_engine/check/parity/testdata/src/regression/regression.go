// Package regression holds a recorded finding only the port makes, and a call to lost, which only the original finds:
// a regression, which nothing can record.
package regression

func gained() {}

func lost() {}

func calls() {
	gained() // port finds: call to gained
	lost()
}
