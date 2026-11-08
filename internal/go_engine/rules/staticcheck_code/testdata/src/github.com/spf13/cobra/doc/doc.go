// Package doc stands in for cobra's doc subpackage, which imports cobra and writes documentation for its commands.
package doc

import "github.com/spf13/cobra"

// Describe is the usage of command.
func Describe(command *cobra.Command) string {
	return command.Use
}
