package standing_facts_test

import (
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
)

// emptyStore is a findings store that holds no answer of any package and reloaded none, so a question that reaches a
// package the run restored is answered a placeholder, and its reader is left unsure.
type emptyStore struct{}

func (emptyStore) Answers(*types.Package) *fact_resolver.Answers { return nil }

func (emptyStore) Reloaded(*types.Package) bool { return false }
