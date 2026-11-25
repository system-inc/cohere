// Package fmt_test has the import path of fmt's external tests, a package of its own that imports fmt, which the
// original's type index holds, so both report here.
package fmt_test

import "fmt"

func reported() string {
	return fmt.Sprint("external")
}
