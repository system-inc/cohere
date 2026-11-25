// Package aliased is main-like: it imports cobra under another name, which names the same package.
package aliased

import commands "github.com/spf13/cobra"

// Root is the command a program runs.
var Root = &commands.Command{Use: "root"}
