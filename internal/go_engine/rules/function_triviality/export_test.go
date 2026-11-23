package function_triviality

import (
	"github.com/system-inc/go-types/types"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
)

// Triviality is triviality, one function's answer, for the tests outside this package.
type Triviality = triviality

// DeclarationPlace is declarationPlace, where a declaration an answer names starts, for the tests outside this
// package.
type DeclarationPlace = declarationPlace

// Ask is ask, function's whole answer as a reader in reader holds it.
func Ask(resolver *fact_resolver.Resolver, reader *fact_resolver.Package, function *types.Func) Triviality {
	return ask(resolver, reader, function)
}

// BodilessTriviality is bodilessTriviality, the answer about function from its types alone.
func BodilessTriviality(function *types.Func) Triviality {
	return bodilessTriviality(function)
}
