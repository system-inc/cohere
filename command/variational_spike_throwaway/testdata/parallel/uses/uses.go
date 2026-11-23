// Package uses reads platform's and status's forked declarations from another package of ours, so the variation
// crosses an import between two checks that run on the schedule.
package uses

import (
	"fmt"
	"strings"

	"example.com/parallel/platform"
	"example.com/parallel/shape"
	"example.com/parallel/status"
)

// Report opens and sizes path, and spells it with the build's platform.
func Report(path string) (string, error) {
	handle, openError := platform.Open(path)
	if openError != nil {
		return "", openError
	}
	size, sizeError := status.Size(path)
	if sizeError != nil {
		return "", sizeError
	}
	described := platform.Describe(handle)
	counted := platform.Doubled()
	outline := shape.Shape{Name: strings.ToUpper(platform.Name), Sides: []int{int(size)}}
	return fmt.Sprint(described, counted, outline, platform.Separator, platform.Lines), nil
}
