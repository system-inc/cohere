// Package use reads native's declarations, C types where its build runs cgo, from another package of ours.
package use

import (
	"fmt"

	"example.com/cgo/native"
)

// Sum adds each of numbers through native and spells the total with the build's kind.
func Sum(numbers ...int) string {
	var total native.Number
	for _, number := range numbers {
		total = native.Add(total, native.Number(number))
	}
	return fmt.Sprintf("%s %d %d", native.Kind, total, native.Area(native.Box{}))
}
