// Package command imports cobra itself, so both judge it a command and leave its ticks alone.
package command

import (
	"time"

	"github.com/spf13/cobra"
)

// Root is the command a program runs.
var Root = &cobra.Command{Use: "root"}

// Found by neither: a command's package.
func returns() {
	<-time.Tick(time.Second)
}
