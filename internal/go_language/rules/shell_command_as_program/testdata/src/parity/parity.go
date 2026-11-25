// Package parity holds every way the query accepts or refuses a call: the callee through parentheses, a method value,
// and functions and methods named Command that are not os/exec's, and the program as a constant written in each way,
// typed, named, inside parentheses, joined, with a space beside a slash or a backslash and without a space, and as
// values that are not constant.
package parity

import (
	"context"
	"os/exec"
)

type program string

const (
	listing               = "ls -l"
	typed         program = "echo hello"
	plain                 = "ls"
	pathWithSpace         = "/Applications/Some App/run"
	windowsPath           = `C:\Program Files\run.exe`
)

type runner struct{}

func (runner) Command(name string, arguments ...string) {}

func Command(name string, arguments ...string) {}

var started = exec.Command("git status")

func init() {
	exec.Command("go build")
}

func constants(arguments []string) {
	exec.Command("ls -l")
	exec.Command("ls")
	exec.Command("")
	exec.Command(" ")
	exec.Command("ls\t-l")
	exec.Command("ls  -l", "-a")
	exec.Command("ls -l", arguments...)
	exec.Command(listing)
	exec.Command(string(typed))
	exec.Command(plain)
	exec.Command(pathWithSpace)
	exec.Command(windowsPath)
	exec.Command("bin/run me")
	exec.Command(`run\ me`)
	exec.Command("ls" + " -l")
	exec.Command(("ls -l"))
	exec.Command(`echo "a b"`)
	exec.Command("日本 語")
}

func notConstant(name string, condition bool) {
	exec.Command(name)
	exec.Command(name + " -l")
	chosen := "ls -l"
	if condition {
		chosen = "ls"
	}
	exec.Command(chosen)
	local := "ls -l"
	exec.Command(local)
}

func callees(parent context.Context) {
	(exec.Command)("ls -l")
	command := exec.Command
	command("ls -l")
	Command("ls -l")
	runner{}.Command("ls -l")
	exec.CommandContext(parent, "ls -l")
	defer exec.Command("ls -l")
	go exec.Command("ls -l")
	func() {
		exec.Command("ls -l")
	}()
	_ = exec.Command("ls -l").Run()
	_ = started
}

func generic[T any](value T) T {
	exec.Command("ls -l")
	return value
}

func instantiated() {
	generic(1)
}
