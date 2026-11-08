package tested_command

import (
	"testing"

	"github.com/spf13/cobra"
)

// Found by neither: nothing ticks here; the file brings cobra into the package's imports.
func TestRoot(t *testing.T) {
	_ = cobra.Command{Use: "root"}
}
