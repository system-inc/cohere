// Package top imports middle, so a question of Top is answered from an answer of middle's.
package top

import "bodiless/middle"

// Top returns what middle.Fresh returns, so the nilness of its result is read from middle's answer about Fresh.
func Top() *int {
	return middle.Fresh()
}
