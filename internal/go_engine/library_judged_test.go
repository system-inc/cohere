package go_engine

import (
	"github.com/system-inc/cohere/internal/go_engine/build_dependence"
)

// libraryJudged is the library's own judgment of a fixture (libraryJudgment): the judgment of the build on the
// host's graph, and the difference it was judged by.
type libraryJudged struct {
	judged     *build_dependence.Dependence
	difference build_dependence.Difference
}
