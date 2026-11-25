// Package child_guard sits at a path other than the configured cure's: its Run and CombinedOutput are not the ones
// the rule knows.
package child_guard

import "os/exec"

type Options struct{}

func Run(command *exec.Cmd, options Options) error { return command.Run() }

func CombinedOutput(command *exec.Cmd, options Options) ([]byte, error) { return nil, command.Run() }
