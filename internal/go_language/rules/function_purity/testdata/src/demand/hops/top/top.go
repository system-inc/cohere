// Package top imports middle alone, so of bottom it holds only what crossed middle: Double, a method, and not Pure.
package top

import "demand/hops/middle"

// Top is pure, through middle.Middle.
func Top(value int) int {
	return middle.Middle(value) + 1
}
