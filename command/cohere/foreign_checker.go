package main

import (
	"fmt"

	"github.com/system-inc/cohere/internal/streams"
	"github.com/system-inc/cohere/internal/types/program"
)

// reportForeignCheckers says, under the COHERE_TEST_FOREIGN_CHECKERS instrument, how many files a walk put on
// a checker other than their own, so a test comparing the two runs can tell the instrument took effect.
func reportForeignCheckers(graph *program.Graph, walked program.Result, files int) {
	if graph.WalkOnForeignCheckers {
		fmt.Fprintf(streams.Errors, "foreign checkers: %d of %d files walked on a checker other than their own\n",
			walked.FilesOnForeignCheckers, files)
		showWarning()
	}
}
