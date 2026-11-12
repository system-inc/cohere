package check

import (
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
)

// answerlessStore is a store that holds no answer and reloaded nothing, as a cold run's findings cache is, so the
// resolver records every answer it computes (fact_resolver.Resolver.Recorded) and every read a package's analysis
// makes of another's.
type answerlessStore struct{}

func (answerlessStore) Answers(*types.Package) *fact_resolver.Answers {
	return nil
}

func (answerlessStore) Reloaded(*types.Package) bool {
	return false
}
