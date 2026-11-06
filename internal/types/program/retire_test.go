package program_test

import (
	"context"
	"crypto/sha256"
	"testing"

	"github.com/system-inc/cohere/internal/types/program"
)

// A retired graph records nothing into the types section, by either of the two paths that record (#sm79kfv).
//
// A graph is retired when the fix phase rewrote files and a successor was built from the new bytes. Its
// check is left to finish rather than cancelled, because cancelling the compiler's whole-program check part
// way panics, so the check can end after the successor's has recorded. What it found describes bytes that no
// longer exist, and recording it last would leave the successor's record overwritten. Each path is paired
// with the same call on a live graph, which must record, so a pass here cannot be a path that never records.
func TestARetiredGraphRecordsNothing(test *testing.T) {
	test.Parallel()
	directory := writeProject(test, map[string]string{
		"tsconfig.json": minimalConfiguration,
		"main.ts":       "import { helper } from './helper';\nexport const value: number = helper();\n",
		"helper.ts":     "export function helper(): number { return 1; }\n",
	})
	backgroundContext := context.Background()
	build := func() *program.Graph {
		test.Helper()
		graph, buildError := program.Build(
			program.Options{ConfigFileName: "tsconfig.json", CurrentDirectory: directory},
		)
		if buildError != nil {
			test.Fatalf("building: %v", buildError)
		}
		graph.Shapes, _ = graph.Signatures(backgroundContext, program.RecordedRealSignatures(graph))
		return graph
	}
	key := sha256.Sum256([]byte("compiler options"))

	// RecordFull, which a full check records through.
	live := build()
	liveReuse := program.NewTypeDiagnosticsReuse(nil, key)
	live.RecordFull(backgroundContext, liveReuse, live.AllDiagnosticParts(backgroundContext))
	recorded := liveReuse.Recorded()
	if recorded == nil {
		test.Fatal("control: a live graph's full check recorded nothing, so the retired case below proves nothing")
	}
	liveReuse.FinishGlobals(true)

	retired := build()
	retired.Retire()
	retiredReuse := program.NewTypeDiagnosticsReuse(nil, key)
	retired.RecordFull(backgroundContext, retiredReuse, retired.AllDiagnosticParts(backgroundContext))
	if retiredReuse.Recorded() != nil {
		test.Error("RecordFull recorded a retired graph's check")
	}

	// CheckReusing, which a check that replays records through, from the section the live graph left.
	replaying := build()
	replayReuse := program.NewTypeDiagnosticsReuse(recorded, key)
	if _, reused := replaying.CheckReusing(backgroundContext, replayReuse); !reused {
		test.Fatal("control: a live graph did not replay the section it just recorded")
	}
	if replayReuse.Recorded() == nil {
		test.Fatal("control: a live graph's replay recorded nothing, so the retired case below proves nothing")
	}

	retiredReplaying := build()
	retiredReplaying.Retire()
	retiredReplayReuse := program.NewTypeDiagnosticsReuse(recorded, key)
	if _, reused := retiredReplaying.CheckReusing(backgroundContext, retiredReplayReuse); !reused {
		test.Fatal("a retired graph did not replay, so whether it records was never asked")
	}
	if retiredReplayReuse.Recorded() != nil {
		test.Error("CheckReusing recorded a retired graph's check")
	}
}
