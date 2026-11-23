package odd_slice_argument_test

import (
	"github.com/system-inc/go-types/types"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
)

// emptyStore is a store that holds no answer and reloaded nothing, so a run under it records every answer it computes
// and every read it makes.
type emptyStore struct{}

func (emptyStore) Answers(*types.Package) *fact_resolver.Answers {
	return nil
}

func (emptyStore) Reloaded(*types.Package) bool {
	return false
}
