// Package top drops what bottom.Double returns, which SA4017 reports while Double is pure.
package top

import "example.com/readset/bottom"

// Drop calls bottom.Double and drops its result.
func Drop() {
	bottom.Double(2)
}
