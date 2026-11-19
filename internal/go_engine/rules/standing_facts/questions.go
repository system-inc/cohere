package standing_facts

import (
	"bytes"
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
)

// questions asks the build each question the host recorded and reads its answer beside the host's, for one pass, on
// one goroutine: the host that says what each key asks, the run's resolver and the package as its reader, the finder
// of the build's own functions, and each verdict by its question's key.
type questions struct {
	host      fact_reach.Host
	resolver  *fact_resolver.Resolver
	reader    *fact_resolver.Package
	functions *functionFinder

	// verdicts memoizes each question's verdict by key, so an answer the host read from another package, asked before
	// the walk, is not asked again when the walk meets it under a body.
	verdicts map[string]fact_reach.Comparison
}

// compare is how the build's answer to question reads beside the host's (see ask), asked once for each key.
func (asked *questions) compare(question fact_reach.Question) fact_reach.Comparison {
	if verdict, isAsked := asked.verdicts[question.Key]; isAsked {
		return verdict
	}
	verdict := asked.ask(question)
	asked.verdicts[question.Key] = verdict
	return verdict
}

// ask is how the build's answer to question reads beside the host's: Equal only when the build has the object the
// host asked of, answers of it for sure, and answers the host's bytes. Everything short of that is Unknown, which the
// walk takes as differing: a key the host cannot explain, a function or a parameter the build does not have, a kind
// this binary does not register, an answer resting on a placeholder.
func (asked *questions) ask(question fact_reach.Question) fact_reach.Comparison {
	asking, known := asked.host.Asking(question.Key)
	if !known {
		return fact_reach.Unknown
	}
	function, found := asked.functions.function(asking.Function)
	if !found {
		return fact_reach.Unknown
	}
	var object types.Object = function
	if asking.Parameter >= 0 {
		parameters := function.Signature().Params()
		if asking.Parameter >= parameters.Len() {
			return fact_reach.Unknown
		}
		object = parameters.At(asking.Parameter)
	}
	if !fact_resolver.Visible(asked.reader, object) {
		// No fact on object reaches this package, so its readers here hold none whatever its own package would
		// answer, and the build's answer is the kind's answer of no fact.
		zero, registered := fact_resolver.ZeroAnswer(asking.Kind)
		if !registered {
			return fact_reach.Unknown
		}
		return comparison(zero, asking.Encoded)
	}
	var encoded []byte
	var sure bool
	var registered bool
	asked.resolver.Question(analyzerName, func() {
		encoded, sure, registered = asked.resolver.Asked(asking.Kind, asked.reader, object)
	})
	if !registered || !sure {
		return fact_reach.Unknown
	}
	return comparison(encoded, asking.Encoded)
}

// comparison is the build's answer beside the host's, each as its resolver encoded it: equal byte for byte, or
// differing.
func comparison(build []byte, host []byte) fact_reach.Comparison {
	if bytes.Equal(build, host) {
		return fact_reach.Equal
	}
	return fact_reach.Differs
}
