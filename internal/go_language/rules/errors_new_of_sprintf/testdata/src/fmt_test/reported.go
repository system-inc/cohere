// Package reported has the import path fmt_test, the path of fmt's external tests, which imports fmt as any package
// does, so the type index knows fmt.Sprintf there and both report.
package reported

import (
	"errors"
	"fmt"
)

func reported(count int) error {
	return errors.New(fmt.Sprintf("%d", count))
}
