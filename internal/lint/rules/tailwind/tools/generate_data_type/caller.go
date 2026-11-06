package main

import (
	"runtime"

	"github.com/system-inc/cohere/internal/lint/rules/tailwind/tools/tool_directory"
)

// toolDirectory is this tool's own source directory, so its sibling scripts are found next to it
// rather than relative to the working directory the command was invoked from. See tool_directory.Of
// for why the runtime.Caller path alone is not enough.
func toolDirectory() (string, error) {
	_, path, _, _ := runtime.Caller(0)
	return tool_directory.Of(path)
}
