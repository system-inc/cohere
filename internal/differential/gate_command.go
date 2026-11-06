package differential

import "strings"

// GateCommand is how to invoke one side.
type GateCommand struct {
	// Name is what appears in the report.
	Name string
	// Program and Arguments are the exact invocation.
	Program   string
	Arguments []string
	// Directory is the working directory, which for both gates is the tree being linted. Getting
	// this wrong is the single most likely way to produce a confident empty result: both gates
	// resolve their config and their file list from here.
	Directory string
}

// String renders the invocation so a reader can rerun it verbatim.
func (command GateCommand) String() string {
	return strings.TrimSpace(command.Program + " " + strings.Join(command.Arguments, " "))
}
