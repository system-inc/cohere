// Package command is main-like: it imports cobra itself.
package command

import "github.com/spf13/cobra"

// Root is the command a program runs.
var Root = &cobra.Command{Use: "root"}
