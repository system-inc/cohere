package fact_resolver_test

import (
	"go/types"

	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_language/fact_resolver"
)

// keepStore is a store that holds stored, when it has one, as keepProbe's answer about middle.Middle, and nothing
// else, and that reloaded nothing.
type keepStore struct {
	stored []byte
}

func (store keepStore) Answers(packageTypes *types.Package) *fact_resolver.Answers {
	if store.stored == nil || packageTypes.Path() != "bodiless/middle" {
		return nil
	}
	return &fact_resolver.Answers{
		Values:   map[string]map[objectpath.Path][]byte{keepProbe.Name: {"Middle": store.stored}},
		Complete: map[string]bool{},
	}
}

func (keepStore) Reloaded(*types.Package) bool {
	return false
}
