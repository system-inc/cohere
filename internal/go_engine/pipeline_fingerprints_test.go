package go_engine

import (
	"io"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/settings"
)

// Each run's pipeline holds fingerprints of its own (#qqyq61s): what the extra builds share of their kept listings'
// fingerprints is held for one run alone, since any file can move between runs, and newPipeline, made once a run, is
// where that is decided. Two pipelines, as two runs of one process make, never share it.
func TestEachRunsPipelineHoldsFingerprintsOfItsOwn(test *testing.T) {
	test.Parallel()
	first := newPipeline(invocation{}, nil, settings.Settings{}, nil, io.Discard, io.Discard, nil)
	second := newPipeline(invocation{}, nil, settings.Settings{}, nil, io.Discard, io.Discard, nil)
	if first.fingerprints == nil || first.fingerprints == second.fingerprints {
		test.Errorf("two runs' pipelines hold fingerprints %p and %p, want one of its own each", first.fingerprints,
			second.fingerprints)
	}
}
