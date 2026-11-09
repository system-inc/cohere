package odd_slice_argument

import (
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
)

// packageFacts is the marks one package's push walk reads and makes: the package's own as the walk marks them, and
// every other package's asked of the resolver, as the checker would have carried them into the package. It serves the
// analyzed package's own walk and the resolver's fallback alike.
type packageFacts struct {
	resolver *fact_resolver.Resolver
	home     *fact_resolver.Package
	recorded map[types.Object]bool
}

func (marks *packageFacts) marked(parameter *types.Var) bool {
	if parameter.Pkg() == marks.home.Types {
		return marks.recorded[parameter]
	}
	return ask(marks.resolver, marks.home, parameter)
}

func (marks *packageFacts) mark(parameter types.Object) {
	marks.recorded[parameter] = true
}
