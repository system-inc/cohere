package audit

import "os/exec"

func target(cmd *exec.Cmd) {
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return
	}
	other()
	_ = cmd.Wait()
	_ = pipe
}

// other returns where stop does, and stops where it stops.
func other() { stop() }
