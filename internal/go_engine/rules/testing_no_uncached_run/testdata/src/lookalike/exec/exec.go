// Package exec is a lookalike of os/exec: its functions share the names and shapes, and run nothing.
package exec

import "context"

// Command has os/exec's name and parameters and is not os/exec's.
func Command(name string, arguments ...string) {}

// CommandContext has os/exec's name and parameters and is not os/exec's.
func CommandContext(requestContext context.Context, name string, arguments ...string) {}
