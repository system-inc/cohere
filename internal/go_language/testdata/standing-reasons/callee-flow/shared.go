package audit

import (
	"context"
	"io"
	"os/exec"
)

func target(ctx context.Context, cmd *exec.Cmd) {
	_, cancel := context.WithCancel(ctx)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return
	}
	go io.Copy(io.Discard, pipe)
	other()
	_ = cmd.Wait()
}

// other returns where stop does, and stops where it stops.
func other() { stop() }
