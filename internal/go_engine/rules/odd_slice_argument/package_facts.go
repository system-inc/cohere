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

	// asking is the row under `--timing` whose questions these are, the analyzer's for the analyzed package's own
	// walk, and empty for a fallback, whose questions are part of the one it falls back in.
	asking string
}

func (marks *packageFacts) marked(parameter *types.Var) bool {
	if parameter.Pkg() == marks.home.Types {
		return marks.recorded[parameter]
	}
	if marks.asking == "" {
		return ask(marks.resolver, marks.home, parameter)
	}
	var marked bool
	marks.resolver.Question(marks.asking, func() { marked = ask(marks.resolver, marks.home, parameter) })
	return marked
}

func (marks *packageFacts) mark(parameter types.Object) {
	marks.recorded[parameter] = true
}
