//go:build !windows

package q

import "fmt"

// platformLog prints its values by format, so it and W wrap fmt.Printf.
func platformLog(format string, values ...any) {
	fmt.Printf(format, values...)
}
