// Package second holds a call to there on the line where first, in a file of the same name, calls here.
package second

func there() {}

func calls() {
	there()
}
