// Package launch embeds the standard library's SysProcAttr, whose fields windows declares in a file of its own, so
// Launch's layout differs on windows though no file of launch does.
package launch

import "syscall"

// Launch is a process's attributes, as the platform's syscall declares them.
type Launch struct {
	syscall.SysProcAttr
}
