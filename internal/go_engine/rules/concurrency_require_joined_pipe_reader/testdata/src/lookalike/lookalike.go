// Package lookalike holds types of its own with exec.Cmd's method names, and a group of its own with a Go method,
// which the rule must never read as a child or as a group that starts a reader.
package lookalike

import (
	"io"
	"os"
	"os/exec"
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

// pool is a project's own group with a Go method, which joins however it joins, so work handed to it is no reader the
// rule judges: only a sync.WaitGroup's or an errgroup.Group's Go starts one.
type pool struct{ started int }

func (current *pool) Go(work func()) { current.started++; go work() }

func pooledReader(command *exec.Cmd) error {
	stdout, _ := command.StdoutPipe()
	_ = command.Start()
	var workers pool
	workers.Go(func() {
		_, _ = io.Copy(os.Stdout, stdout)
	})
	return command.Wait()
}
