// Package main is a program, whose ticks both leave alone.
package main

import "time"

// Found by neither: a program's package.
func main() {
	<-time.Tick(time.Second)
}
