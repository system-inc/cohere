// Package lookalike holds types of its own with exec.Cmd's method names, which the rule must never read as a child.
package lookalike

import (
	"io"
	"os"
	"strings"
)

type child struct{}

func (child) StdoutPipe() (io.Reader, error) { return strings.NewReader(""), nil }
func (child) Start() error                   { return nil }
func (child) Wait() error                    { return nil }

func copyThenWait(process child) error {
	stdout, _ := process.StdoutPipe()
	_ = process.Start()
	go io.Copy(os.Stdout, stdout)
	return process.Wait()
}
